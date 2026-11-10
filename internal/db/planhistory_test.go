package db

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/store"
)

// planhistory_test.go -- T27 (migrations 00097 and 00098, ADR 0043): a tenant's plan history and
// the plan IN FORCE for a billing month.
//
// Every test runs in one owner transaction that is rolled back (opTx), and takes tappa_app's or
// tappa_opdefiner's identity inside savepoints (opAs). The fixture tenants are inserted by the
// owner WITH their plan, so the history holds only what each test writes: a tenant inserted that
// way has no row until its plan changes (00097 section 3).

const (
	phZone    = "Europe/Malta"
	phSignup  = "2026-01-10T09:00:00Z"
	phPlanFix = "00097_record_each_tenants_plan_history.sql"
	phOpFix   = "00098_price_the_operators_billing_read_under_the_plan_in_force.sql"
)

// phTenant inserts a tenant as the owner -- its plan, its zone, its signup instant -- with a venue
// and an owner account (billing_periods.closed_by needs one). No history row is written.
func phTenant(t *testing.T, ctx context.Context, tx pgx.Tx, plan, signup string) opBillingFixture {
	t.Helper()
	f := opBillingFixture{id: uuid.New(), zone: phZone, admin: uuid.New(), loc: uuid.New()}
	f.name = "T27 " + plan + " " + f.id.String()[:8]
	created, err := time.Parse(time.RFC3339, signup)
	if err != nil {
		t.Fatal(err)
	}
	f.created = created
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure, plan, timezone, price_per_employee_month, created_at)
		  VALUES ($1, $2, $3, 'restaurant', 'single', $4, $5, 1.10, $6)`,
			[]any{f.id, f.name, "VAT-T27-" + f.id.String(), plan, phZone, created}},
		{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'T27 door')`, []any{f.loc, f.id}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		  VALUES ($1, $2, 'T27 owner', $3, $4, 'owner', 'active')`,
			[]any{f.admin, f.id, "t27-" + f.admin.String()[:12] + "@example.test", opFakeDigest("b")}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("plan-history fixture: %s: %v", strings.Fields(s.sql)[2], err)
		}
	}
	return f
}

// phHistory writes history rows as the owner, as the product never does -- effective_from is
// otherwise the trigger's wall clock -- so a test can place a change at a chosen instant.
func phHistory(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, rows ...[2]string) {
	t.Helper()
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_plan_history (tenant_id, plan, effective_from) VALUES ($1, $2, $3::timestamptz)`,
			tenant, r[0], r[1]); err != nil {
			t.Fatalf("history row %v: %v", r, err)
		}
	}
}

// phRows reads a tenant's history as the owner, oldest first: "plan@effective_from by changed_by".
func phRows(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) []string {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT plan || '@' || to_char(effective_from AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US') ||
	                                  ' by ' || coalesce(changed_by::text, '-')
	                             FROM tenant_plan_history WHERE tenant_id = $1 ORDER BY effective_from`, tenant)
	if err != nil {
		t.Fatalf("read the history: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read the history: %v", err)
	}
	return out
}

// phPreview is the tenant's own PreviewBillingPeriod for a month, as tappa_app in its context.
func phPreview(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, month string) store.PreviewBillingPeriodRow {
	t.Helper()
	m, err := time.Parse("2006-01", month)
	if err != nil {
		t.Fatal(err)
	}
	return opBillingTenantPath(t, ctx, tx, tenant, pgtype.Date{Time: m, Valid: true}).preview
}

// TestRLS_TenantPlanHistory_TenantIsolation: tenant A's context reads A's history and nothing of
// B's, B's the reverse, no context reads nothing -- the reads carry NO tenant filter (CLAUDE.md §6:
// with one, 0 rows would be the WHERE's doing, and the test green with RLS off). CONTROL: the same
// read with row level security switched off (inside the rolled-back transaction) sees both, so the
// isolation above is the policy's. And the WITH CHECK: tappa_app has no INSERT (00097), so the
// write half is proven with one granted inside the transaction -- B's id under A's context is
// refused by the policy (42501, "row-level security"), A's own id goes through.
func TestRLS_TenantPlanHistory_TenantIsolation(t *testing.T) {
	ctx, tx := opTx(t)
	a := phTenant(t, ctx, tx, "founding", phSignup)
	b := phTenant(t, ctx, tx, "founding", phSignup)
	for _, f := range []opBillingFixture{a, b} {
		if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'standard' WHERE id = $1`, f.id); err != nil {
			t.Fatalf("change %s's plan: %v", f.name, err)
		}
	}
	read := func(ctxTenant *uuid.UUID, rlsOff bool) map[uuid.UUID]int {
		t.Helper()
		seen := map[uuid.UUID]int{}
		if rlsOff {
			if _, err := tx.Exec(ctx, `SAVEPOINT rls_off; ALTER TABLE tenant_plan_history NO FORCE ROW LEVEL SECURITY;
			                           ALTER TABLE tenant_plan_history DISABLE ROW LEVEL SECURITY`); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT rls_off`); err != nil {
					t.Fatal(err)
				}
			}()
		}
		if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			// No context is the empty GUC (CLAUDE.md §6: once written it never returns to NULL), and
			// it is written every time: a set_config made in an earlier, released savepoint is still
			// the transaction's.
			value := ""
			if ctxTenant != nil {
				value = ctxTenant.String()
			}
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, value); err != nil {
				return err
			}
			rows, err := sp.Query(ctx, `SELECT tenant_id FROM tenant_plan_history`)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			for _, id := range ids {
				seen[id]++
			}
			return err
		}); err != nil {
			t.Fatalf("read as tappa_app: %v", err)
		}
		return seen
	}
	for _, c := range []struct {
		name     string
		ctx      *uuid.UUID
		own      uuid.UUID
		stranger uuid.UUID
	}{{"A", &a.id, a.id, b.id}, {"B", &b.id, b.id, a.id}} {
		seen := read(c.ctx, false)
		if seen[c.own] != 2 || len(seen) != 1 {
			t.Errorf("tenant %s's context reads %v; want its own two rows (the first plan and the change) and no other tenant's", c.name, seen)
		}
		if seen[c.stranger] != 0 {
			t.Errorf("tenant %s's context reads %d of the other tenant's rows", c.name, seen[c.stranger])
		}
	}
	if seen := read(nil, false); len(seen) != 0 {
		t.Errorf("no tenant context reads %v; want nothing", seen)
	}
	if seen := read(&a.id, true); seen[a.id] != 2 || seen[b.id] != 2 {
		t.Fatalf("CONTROL: with row level security off, A's context reads %v -- the isolation above would not be the policy's", seen)
	}

	if _, err := tx.Exec(ctx, `GRANT INSERT (tenant_id, plan, effective_from) ON tenant_plan_history TO tappa_app`); err != nil {
		t.Fatal(err)
	}
	insertAs := func(ctxTenant, row uuid.UUID) error {
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, ctxTenant.String()); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, `INSERT INTO tenant_plan_history (tenant_id, plan, effective_from)
			                        VALUES ($1, 'founding', clock_timestamp())`, row)
			return err
		})
	}
	err := insertAs(a.id, b.id)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != sqlstateInsufficientPrivi || !strings.Contains(pg.Message, "row-level security") {
		t.Fatalf("tappa_app writing B's id under A's context answered %v; want the policy's WITH CHECK (42501, row-level security)", err)
	}
	if err := insertAs(a.id, a.id); err != nil {
		t.Fatalf("CONTROL: tappa_app writing A's own id under A's context (with the INSERT granted here): %v -- the refusal above would not be the WITH CHECK", err)
	}
}

// TestTenantPlanHistory_IsAppendOnlyForEveryRole: tappa_app may read three columns and do nothing
// else -- no INSERT, UPDATE, DELETE or TRUNCATE (42501), none of the other three columns, and no
// plan change of its own; the owner, a superuser, meets tappa_forbid_mutation on UPDATE and DELETE
// (00097 section 1). The owner's TRUNCATE is internal/db/appendonly_truncate_test.go's (its
// truncateTargets name the table), where the lock it takes is bounded against the deadlock detector.
func TestTenantPlanHistory_IsAppendOnlyForEveryRole(t *testing.T) {
	ctx, tx := opTx(t)
	f := phTenant(t, ctx, tx, "founding", phSignup)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'standard' WHERE id = $1`, f.id); err != nil {
		t.Fatal(err)
	}
	asApp := func(sql string, args ...any) error {
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}
	if err := asApp(`SELECT tenant_id, plan, effective_from FROM tenant_plan_history WHERE tenant_id = $1`, f.id); err != nil {
		t.Fatalf("CONTROL: tappa_app reads the three columns: %v", err)
	}
	for _, sql := range []string{
		`SELECT changed_by FROM tenant_plan_history WHERE tenant_id = $1`,
		`SELECT id FROM tenant_plan_history WHERE tenant_id = $1`,
		`SELECT recorded_at FROM tenant_plan_history WHERE tenant_id = $1`,
		`INSERT INTO tenant_plan_history (tenant_id, plan, effective_from) VALUES ($1, 'founding', clock_timestamp())`,
		`UPDATE tenant_plan_history SET plan = 'founding' WHERE tenant_id = $1`,
		`DELETE FROM tenant_plan_history WHERE tenant_id = $1`,
		`UPDATE tenants SET plan = 'founding' WHERE id = $1`,
	} {
		opWant(t, asApp(sql, f.id), sqlstateInsufficientPrivi, "tappa_app: "+sql)
	}
	opWant(t, asApp(`TRUNCATE tenant_plan_history`), sqlstateInsufficientPrivi, "tappa_app: TRUNCATE")
	for _, sql := range []string{
		`UPDATE tenant_plan_history SET plan = 'founding' WHERE tenant_id = $1`,
		`UPDATE tenant_plan_history SET effective_from = effective_from - interval '1 month' WHERE tenant_id = $1`,
		`DELETE FROM tenant_plan_history WHERE tenant_id = $1`,
	} {
		assertAppendOnlyTrigger(t, "owner "+sql, opTry(t, ctx, tx, sql, f.id))
	}
	if got := phRows(t, ctx, tx, f.id); len(got) != 2 {
		t.Fatalf("after the refusals the history is %v; want its two rows untouched", got)
	}
}

// TestTenantPlanHistory_APlanChangeRecordsItsFirstPlanItsInstantAndItsOperator: the tenants trigger
// (00097 section 3), whoever changes the plan.
//   - a tenant with no row: the change writes its FIRST plan from created_at, then the new plan from
//     the wall clock (between the clocks read either side), changed_by NULL;
//   - with tappa.plan_changed_by set (transaction-local, OP-17's protocol -- ADR 0043 §4) to a
//     platform_admins id, the next change names that operator; a value that is no uuid (22P02) and
//     a uuid that names no operator (23503, 00098's key) refuse the whole UPDATE -- plan unchanged;
//   - an UPDATE that leaves the plan as it is, and one of another column, write nothing.
func TestTenantPlanHistory_APlanChangeRecordsItsFirstPlanItsInstantAndItsOperator(t *testing.T) {
	ctx, tx := opTx(t)
	f := phTenant(t, ctx, tx, "founding", phSignup)
	if got := phRows(t, ctx, tx, f.id); len(got) != 0 {
		t.Fatalf("PREMISE: a tenant inserted after 00097 has %v; want no row before its first change", got)
	}
	var before, after time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'standard' WHERE id = $1`, f.id); err != nil {
		t.Fatalf("change the plan as the owner: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	var first, change time.Time
	var firstPlan, changePlan string
	if err := tx.QueryRow(ctx, `
		SELECT (array_agg(plan ORDER BY effective_from))[1], (array_agg(effective_from ORDER BY effective_from))[1],
		       (array_agg(plan ORDER BY effective_from))[2], (array_agg(effective_from ORDER BY effective_from))[2]
		  FROM tenant_plan_history WHERE tenant_id = $1 AND changed_by IS NULL HAVING count(*) = 2`, f.id).
		Scan(&firstPlan, &first, &changePlan, &change); err != nil {
		t.Fatalf("the change wrote %v; want two rows, no operator: %v", phRows(t, ctx, tx, f.id), err)
	}
	if firstPlan != "founding" || !first.Equal(f.created) || changePlan != "standard" || change.Before(before) || change.After(after) {
		t.Errorf("the history is %s@%s, %s@%s; want founding@%s (the signup) and standard between %s and %s (the wall clock)",
			firstPlan, first.UTC(), changePlan, change.UTC(), f.created.UTC(), before.UTC(), after.UTC())
	}

	op := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `SELECT set_config('tappa.plan_changed_by', $1, true)`, op.id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'founding' WHERE id = $1`, f.id); err != nil {
		t.Fatalf("change the plan with the operator named: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenant_plan_history WHERE tenant_id = $1 AND plan = 'founding' AND changed_by = $2`, f.id, op.id); n != 1 {
		t.Errorf("the change with tappa.plan_changed_by set wrote %v; want one founding row naming the operator", phRows(t, ctx, tx, f.id))
	}

	for _, c := range []struct{ value, code, what string }{
		{"not-a-uuid", "22P02", "a value that is no uuid"},
		{uuid.NewString(), "23503", "a uuid that names no operator"},
	} {
		err := func() error {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Fatal(err)
				}
			}()
			if _, err := sp.Exec(ctx, `SELECT set_config('tappa.plan_changed_by', $1, true)`, c.value); err != nil {
				t.Fatal(err)
			}
			_, err = sp.Exec(ctx, `UPDATE tenants SET plan = 'standard' WHERE id = $1`, f.id)
			return err
		}()
		opWant(t, err, c.code, "a plan change attributed to "+c.what)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants WHERE id = $1 AND plan = 'founding'`, f.id); n != 1 {
		t.Errorf("a refused attribution moved the plan")
	}

	n := len(phRows(t, ctx, tx, f.id))
	for _, sql := range []string{
		`UPDATE tenants SET plan = 'founding' WHERE id = $1`,
		`UPDATE tenants SET name = name || ' renamed' WHERE id = $1`,
	} {
		if _, err := tx.Exec(ctx, sql, f.id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	if got := phRows(t, ctx, tx, f.id); len(got) != n {
		t.Errorf("an UPDATE that kept the plan wrote history: %v (had %d rows)", got, n)
	}
}

// TestPlanInForce_AMonthIsGovernedByThePlanAtItsStart is ADR 0043 §2, through the tenant's own
// PreviewBillingPeriod (as tappa_app) and the reader itself. One Malta tenant signed up on founding
// on 2026-01-10 (founding's free window: January to March), each with a move to standard placed
// somewhere else:
//   - mid-February: February stays free (the change lands inside it), March is chargeable --
//     founding's window would have reached it;
//   - EXACTLY at March's first instant (2026-02-28T23:00Z, Malta's midnight): March is standard's;
//   - thirty minutes later (2026-02-28T23:30Z -- March in Malta, still February in UTC): March is
//     still founding's -- a reader that took the UTC calendar month would have said standard;
//   - a tenant with no row at all is governed by tenants.plan (standard: January is chargeable).
//
// January (the signup month) starts before every tenant's first row and is governed by it.
func TestPlanInForce_AMonthIsGovernedByThePlanAtItsStart(t *testing.T) {
	ctx, tx := opTx(t)
	type want struct {
		month, plan string
		free        bool
	}
	cases := []struct {
		name   string
		plan   string // tenants.plan, the plan today
		change string // the instant of the move to standard ("" = no history at all)
		months []want
	}{
		{"mid-February", "standard", "2026-02-15T12:00:00Z",
			[]want{{"2026-01", "founding", true}, {"2026-02", "founding", true}, {"2026-03", "standard", false}, {"2026-04", "standard", false}}},
		{"at March's first instant", "standard", "2026-02-28T23:00:00Z",
			[]want{{"2026-02", "founding", true}, {"2026-03", "standard", false}}},
		{"thirty minutes into Malta's March", "standard", "2026-02-28T23:30:00Z",
			[]want{{"2026-02", "founding", true}, {"2026-03", "founding", true}, {"2026-04", "standard", false}}},
		{"no history row", "standard", "",
			[]want{{"2026-01", "standard", false}, {"2026-03", "standard", false}}},
	}
	for _, c := range cases {
		f := phTenant(t, ctx, tx, c.plan, phSignup)
		if c.change != "" {
			phHistory(t, ctx, tx, f.id, [2]string{"founding", phSignup}, [2]string{"standard", c.change})
		}
		for _, w := range c.months {
			p := phPreview(t, ctx, tx, f.id, w.month)
			if p.Plan != w.plan || p.FreePeriod != w.free {
				t.Errorf("%s, %s: preview plan %s free %v; want %s, free %v", c.name, w.month, p.Plan, p.FreePeriod, w.plan, w.free)
			}
			var direct string
			if err := tx.QueryRow(ctx, `SELECT public.tappa_plan_in_force($1, $2, $3)`, f.id, p.PeriodFrom, c.plan).Scan(&direct); err != nil {
				t.Fatal(err)
			}
			if direct != w.plan {
				t.Errorf("%s, %s: tappa_plan_in_force at %s = %s, want %s", c.name, w.month, p.PeriodFrom.UTC(), direct, w.plan)
			}
		}
	}
	// PREMISE of the third case, measured rather than assumed: 23:30Z on the 28th is in Malta's
	// March and in UTC's February.
	var malta, utc string
	if err := tx.QueryRow(ctx, `SELECT to_char(timestamptz '2026-02-28T23:30:00Z' AT TIME ZONE $1, 'YYYY-MM'),
	                                  to_char(timestamptz '2026-02-28T23:30:00Z' AT TIME ZONE 'UTC', 'YYYY-MM')`, phZone).Scan(&malta, &utc); err != nil {
		t.Fatal(err)
	}
	if malta != "2026-03" || utc != "2026-02" {
		t.Fatalf("PREMISE: 2026-02-28T23:30Z is %s in Malta and %s in UTC; want 2026-03 and 2026-02", malta, utc)
	}
}

// TestPlanInForce_TheCloseFreezesThePlanInForceNotTodays is T27's harm itself: February has ended
// and is not closed; the plan moves to standard after it (in March); closing February through the
// product's own CloseBillingPeriod freezes founding and a free month -- not today's standard.
// CONTROL: the same close with the reader taken out of the arithmetic (tappa_plan_in_force made to
// return tenants.plan, inside a savepoint) freezes standard -- so the frozen founding is the
// reader's doing.
func TestPlanInForce_TheCloseFreezesThePlanInForceNotTodays(t *testing.T) {
	ctx, tx := opTx(t)
	closeFeb := func(f opBillingFixture) (string, bool) {
		t.Helper()
		var plan string
		var free bool
		if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
				return err
			}
			row, err := store.New(sp).CloseBillingPeriod(ctx, store.CloseBillingPeriodParams{
				ClosedBy: f.admin, PeriodMonth: pgtype.Date{Time: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC), Valid: true}, TenantID: f.id})
			plan, free = row.Plan, row.FreePeriod
			return err
		}); err != nil {
			t.Fatalf("close February at %s: %v", f.name, err)
		}
		return plan, free
	}
	f := phTenant(t, ctx, tx, "standard", phSignup)
	phHistory(t, ctx, tx, f.id, [2]string{"founding", phSignup}, [2]string{"standard", "2026-03-10T10:00:00Z"})
	if plan, free := closeFeb(f); plan != "founding" || !free {
		t.Errorf("February closed as %s, free %v; the plan in force at its start is founding (free)", plan, free)
	}

	g := phTenant(t, ctx, tx, "standard", phSignup)
	phHistory(t, ctx, tx, g.id, [2]string{"founding", phSignup}, [2]string{"standard", "2026-03-10T10:00:00Z"})
	if _, err := tx.Exec(ctx, `SAVEPOINT no_reader;
		CREATE OR REPLACE FUNCTION public.tappa_plan_in_force(p_tenant_id uuid, p_at timestamptz, p_current_plan text)
		    RETURNS text LANGUAGE sql STABLE SET search_path = pg_catalog, pg_temp AS $$ SELECT p_current_plan $$`); err != nil {
		t.Fatal(err)
	}
	plan, free := closeFeb(g)
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT no_reader`); err != nil {
		t.Fatal(err)
	}
	if plan != "standard" || free {
		t.Fatalf("CONTROL: with the reader returning tenants.plan February closed as %s, free %v; want standard, chargeable", plan, free)
	}
}

// TestPlanHistory00097_BackfillsOneRowPerTenantAndTheDownRefusesARecordedChange walks 00098's and
// 00097's Downs and 00097's Up inside the transaction (rolled back):
//   - with a recorded change (a tenant with two rows), 00097's Down refuses (55000 naming 00097)
//     and changes nothing;
//   - without one (the changed tenants' rows removed inside the transaction, as the identity walks
//     do), it drops the table, the trigger and both functions;
//   - the Up then writes exactly one row per tenant: its plan, from its created_at, no operator --
//     for every tenant in the database, fixtures of other tests included;
//   - 00098's Up gives the operator half back (the key, the definer's read).
func TestPlanHistory00097_BackfillsOneRowPerTenantAndTheDownRefusesARecordedChange(t *testing.T) {
	ctx, tx := opTx(t)
	up97, down97 := opMigrationSections(t, phPlanFix)
	up98, down98 := opMigrationSections(t, phOpFix)
	tables := append(downTables(down97), downTables(down98)...)
	slices.Sort(tables)
	tables = slices.Compact(tables)
	lockForDowns(t, ctx, tx, tables)

	f := phTenant(t, ctx, tx, "founding", phSignup)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'standard' WHERE id = $1`, f.id); err != nil {
		t.Fatal(err)
	}
	opRunSection(t, ctx, tx, down98, phOpFix+" Down")
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runErr := holdingNestedRun(t, ctx, sp, down97)
	var pg *pgconn.PgError
	if !errors.As(runErr, &pg) || pg.Code != sqlstatePrerequisiteState || !strings.Contains(pg.Message, "migration 00097 Down") {
		t.Fatalf("00097's Down over a recorded change answered %v; want 55000 naming 00097", runErr)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got := phRows(t, ctx, tx, f.id); len(got) != 2 {
		t.Fatalf("the refused Down changed the history: %v", got)
	}

	aiLiftLaterDownRefusals(t, ctx, tx, []aiLaterMigration{{name: phPlanFix}})
	opRunSection(t, ctx, tx, down97, phPlanFix+" Down")
	if n := opInt(t, ctx, tx, `SELECT (to_regclass('public.tenant_plan_history') IS NOT NULL)::int
	                               + (to_regprocedure('public.tappa_plan_in_force(uuid, timestamptz, text)') IS NOT NULL)::int
	                               + (to_regprocedure('public.tappa_record_plan_change()') IS NOT NULL)::int
	                               + (SELECT count(*) FROM pg_trigger WHERE tgname = 'tenants_plan_history')::int`); n != 0 {
		t.Fatalf("after 00097's Down %d of its four objects remain", n)
	}

	opRunSection(t, ctx, tx, up97, phPlanFix+" Up")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants t
	                            WHERE (SELECT count(*) FROM tenant_plan_history h
	                                    WHERE h.tenant_id = t.id AND h.plan = t.plan AND h.effective_from = t.created_at
	                                      AND h.changed_by IS NULL) <> 1`); n != 0 {
		t.Errorf("after the Up %d tenants lack exactly one backfilled row (their plan, from created_at)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT (SELECT count(*) FROM tenant_plan_history) - (SELECT count(*) FROM tenants)`); n != 0 {
		t.Errorf("after the Up the history holds %d rows more than there are tenants", n)
	}
	if got := phRows(t, ctx, tx, f.id); len(got) != 1 || !strings.HasPrefix(got[0], "standard@2026-01-10T09:00:00") {
		t.Errorf("the changed tenant's backfilled history is %v; want its current plan from its signup", got)
	}
	opRunSection(t, ctx, tx, up98, phOpFix+" Up")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_constraint WHERE conname = 'tenant_plan_history_changed_by_fkey'`); n != 1 {
		t.Errorf("after 00098's Up the key is missing")
	}
}

// TestTenantPlanHistory_ABeforeTriggerThatMovesThePlanStillLeavesItsRow is the security review's
// DÜŞÜK (2026-11-09): the tenants trigger is AFTER UPDATE with a WHEN on the stored plan, not
// `UPDATE OF plan`. A BEFORE trigger (created here, in the rolled-back transaction) that sets
// NEW.plan on an UPDATE naming only the timezone still leaves the change's history row.
// CONTROL: the same UPDATE with the trigger redefined as `UPDATE OF plan` (in a savepoint) moves
// the plan and writes no row -- so the row above is the definition's doing.
func TestTenantPlanHistory_ABeforeTriggerThatMovesThePlanStillLeavesItsRow(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `
		CREATE FUNCTION public.zz_t27_move_plan() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN NEW.plan := 'standard'; RETURN NEW; END $$;
		CREATE TRIGGER zz_t27_move_plan BEFORE UPDATE ON public.tenants
		    FOR EACH ROW WHEN (current_setting('tappa.zz_t27', true) = 'on') EXECUTE FUNCTION public.zz_t27_move_plan();
		SELECT set_config('tappa.zz_t27', 'on', true)`); err != nil {
		t.Fatalf("the plan-moving BEFORE trigger: %v", err)
	}
	moved := func(f opBillingFixture) (plan string, rows int) {
		t.Helper()
		if _, err := tx.Exec(ctx, `UPDATE tenants SET timezone = 'Europe/Rome' WHERE id = $1`, f.id); err != nil {
			t.Fatalf("UPDATE the timezone: %v", err)
		}
		if err := tx.QueryRow(ctx, `SELECT plan FROM tenants WHERE id = $1`, f.id).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		return plan, len(phRows(t, ctx, tx, f.id))
	}
	f := phTenant(t, ctx, tx, "founding", phSignup)
	if plan, rows := moved(f); plan != "standard" || rows != 2 {
		t.Errorf("a BEFORE trigger moved the plan to %s on a timezone UPDATE and the history holds %d rows (%v); want standard and two (the first plan and the change)",
			plan, rows, phRows(t, ctx, tx, f.id))
	}

	g := phTenant(t, ctx, tx, "founding", phSignup)
	if _, err := tx.Exec(ctx, `SAVEPOINT of_plan;
		CREATE OR REPLACE TRIGGER tenants_plan_history AFTER UPDATE OF plan ON public.tenants FOR EACH ROW
		    WHEN (OLD.plan IS DISTINCT FROM NEW.plan) EXECUTE FUNCTION public.tappa_record_plan_change()`); err != nil {
		t.Fatal(err)
	}
	plan, rows := moved(g)
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT of_plan`); err != nil {
		t.Fatal(err)
	}
	if plan != "standard" || rows != 0 {
		t.Fatalf("CONTROL: with `UPDATE OF plan` the timezone UPDATE left plan %s and %d history rows; want standard and none -- the test would not tell the definitions apart", plan, rows)
	}
}

// TestRestoreVerify_PlanHistoryQuery runs scripts/pg-restore-verify.sh section 7's PLAN_HISTORY_SQL,
// cut from the script, in a rolled-back transaction: a plan change through the trigger adds no
// count; a change made with the trigger bypassed (session_replication_role = replica, the owner's
// way past it -- ADR 0043 §3) on a tenant that has a history adds exactly one. The count before is
// taken as the baseline: other tests' committed tenants are in the database.
func TestRestoreVerify_PlanHistoryQuery(t *testing.T) {
	q := restoreTriggerSQL(t, "PLAN_HISTORY_SQL")
	ctx, tx := opTx(t)
	count := func() int64 { t.Helper(); return opInt(t, ctx, tx, q) }
	base := count()
	f := phTenant(t, ctx, tx, "founding", phSignup)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'standard' WHERE id = $1`, f.id); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != base {
		t.Fatalf("a change through the trigger moved the count %d -> %d; want no change", base, n)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'founding' WHERE id = $1`, f.id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = origin`); err != nil {
		t.Fatal(err)
	}
	if got := phRows(t, ctx, tx, f.id); len(got) != 2 {
		t.Fatalf("PREMISE: the bypassed change wrote history (%v); replica should have skipped the trigger", got)
	}
	if n := count(); n != base+1 {
		t.Errorf("a change that bypassed the trigger moved the count %d -> %d; want exactly one more", base, n)
	}
}
