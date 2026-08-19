package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// Migration 00021 part 4 (T17) -- audit_log (tenant_id, target).
//
// db/queries/audit.sql's header states the problem it could not fix at the time:
// ListPlaqueHistory's @row_limit bounds the OUTPUT, not the WORK. audit_log had
// two indexes (pkey and (tenant_id, at DESC)), so a plaque card fetched every
// audit row the TENANT owns and discarded all of them. Measured 2026-08-19 as
// tappa_app with the tenant GUC set, 3 688 rows in that tenant of 193 064:
//
//	BEFORE  Bitmap Heap Scan, Rows Removed by Filter: 3 688,
//	        Heap Blocks: exact=1 184, Buffers: shared hit=103 read=1 113
//	        6.219 ms cold / 2.082 / 1.963 / 1.948 ms warm
//	AFTER   Index Scan using audit_log_tenant_target_idx,
//	        Buffers: shared read=3
//	        0.194 ms cold / 0.154 / 0.159 / 0.164 ms warm
//
// 🔴 TWO TESTS, BECAUSE THE INDEX HAS TWO PROPERTIES AND ONE ASSERTION CANNOT
// SEE BOTH. This was MEASURED, and the single-assertion version that used to live
// here was wrong about both halves:
//
//   - SHAPE (does the index have the right columns, in the right order) is a
//     CATALOG fact. It is asserted below by TestAuditIndex00021_IndexShape.
//     The previous version claimed a plan assertion covered it -- its comment
//     said "reordering the columns ... shows up as a Bitmap Heap Scan again".
//     MEASURED AND FALSE: the index was recreated under the SAME NAME as
//     (target, tenant_id), which destroys the tenant_id-leads property section 6
//     and redline R5 require, and the plan test stayed GREEN -- because a btree
//     on (target, tenant_id) still serves `target = $2` from its leading column,
//     so the planner still reaches the rows through an index of that name. A
//     plan can report WHICH index was used; it cannot report what the index IS.
//
//   - USABILITY (will the planner actually choose it) is a COST fact, and cost
//     depends on statistics, which depend on data. The previous version asserted
//     it against a tenant holding ONE row and was green only by accident of this
//     machine's 194 770 accumulated audit rows. Measured on a freshly migrated
//     database at several volumes, running the old test verbatim:
//
//     audit_log rows   101    122    223    424   1 425
//     verdict          FAIL   FAIL   FAIL   FAIL  FAIL
//
//     -- and at 424 and 1 425 rows the plan was not even the "no index" fallback
//     the old failure message named: it was `Index Scan using
//     audit_log_tenant_at_idx`, i.e. the OTHER index, chosen because it also
//     satisfies `ORDER BY at DESC LIMIT 50` and the tenant was estimated to hold
//     one row. On a tenant with one audit row that is the CORRECT plan. So the
//     old test did not have a threshold problem; it was asking a cost question
//     under conditions in which the answer it demanded would have been wrong.
// ============================================================================

// auditTargetIndex is the object under test. Named once so the two tests and
// their messages cannot drift apart.
const auditTargetIndex = "audit_log_tenant_target_idx"

// TestAuditIndex00021_IndexShape asserts what the index IS, from the catalog,
// with no reference to any plan, any row count or any statistic. It is the
// assertion that survives an empty database and the one that fails when somebody
// recreates the index with the columns the other way round.
//
// 🔴 COLUMN ORDER IS THE POINT, NOT A DETAIL. CLAUDE.md section 6 and redline R5
// both require tenant_id to LEAD every index on a tenant-scoped table: the
// leading column is what lets an RLS-filtered, tenant-scoped query use the index
// at all, and (target, tenant_id) would additionally hand out a physical path
// ordered by a value that is not the tenant boundary.
func TestAuditIndex00021_IndexShape(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	ctx := context.Background()

	// pg_indexes is a view, so a missing index is zero rows rather than an
	// error -- which is the failure this must report clearly, because it is
	// also what `DROP INDEX` looks like.
	var def string
	err := app.pool.QueryRow(ctx,
		`SELECT indexdef FROM pg_indexes
		  WHERE schemaname = 'public' AND tablename = 'audit_log' AND indexname = $1`,
		auditTargetIndex).Scan(&def)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("index %s does not exist on public.audit_log (migration 00021 part 4, backlog T17)", auditTargetIndex)
	}
	if err != nil {
		t.Fatalf("read pg_indexes: %v", err)
	}

	// The KEY columns in index order, straight from the catalog. Read separately
	// from indexdef so the failure message can name the order that is actually
	// there instead of leaving a reader to diff two long CREATE INDEX strings.
	var cols string
	if err := app.pool.QueryRow(ctx,
		`SELECT string_agg(a.attname, ', ' ORDER BY k.ord)
		   FROM pg_index i
		   CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
		   JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		  WHERE i.indexrelid = to_regclass('public.' || $1)
		    AND k.ord <= i.indnkeyatts`,
		auditTargetIndex).Scan(&cols); err != nil {
		t.Fatalf("read index key columns: %v", err)
	}
	if cols != "tenant_id, target" {
		t.Fatalf("%s indexes (%s); it must index (tenant_id, target) IN THAT ORDER.\n"+
			"tenant_id has to LEAD (CLAUDE.md section 6, redline R5): it is the column RLS filters on, "+
			"and an index that leads with target is a physical path ordered by something that is not the "+
			"tenant boundary. NOTE: a plan assertion cannot catch this -- measured, a (target, tenant_id) "+
			"index of the same name is still CHOSEN for this query, so only the catalog can tell you.",
			auditTargetIndex, cols)
	}

	// Belt over the braces: indexdef also pins btree, the schema, the table, and
	// the absence of a WHERE clause / non-default operator classes -- things the
	// column list alone would not notice.
	want := fmt.Sprintf("CREATE INDEX %s ON public.audit_log USING btree (tenant_id, target)", auditTargetIndex)
	if def != want {
		t.Fatalf("unexpected definition for %s:\n  have: %s\n  want: %s", auditTargetIndex, def, want)
	}
}

// TestAuditIndex00021_PlaqueHistoryUsesTheIndex runs ListPlaqueHistory's exact
// predicate under EXPLAIN and requires the planner to reach it through the index
// -- under the conditions the index EXISTS for, which it builds itself instead of
// hoping the database it is pointed at happens to have them.
//
// 🔴 THE WHOLE PROBE IS ONE TRANSACTION THAT IS ROLLED BACK, AND EVERY PART OF
// THAT SENTENCE IS LOAD-BEARING:
//
//   - it INSERTS a tenant with a realistic audit volume, because T17's problem
//     only exists for a tenant that HAS one (measured: with the tenant holding 0
//     extra rows on a fresh database the planner picks audit_log_tenant_at_idx,
//     and it is right to);
//   - it then runs ANALYZE, so the choice is made from statistics this test
//     created rather than statistics some earlier `make test` left behind. This
//     is why the test needs the OWNER role: ANALYZE requires table ownership;
//   - it ROLLS BACK, so the rows and the statistics both disappear. Measured, in
//     this order on the same database: N=100 -> Index Scan using
//     audit_log_tenant_target_idx; rollback; N=0 -> Index Scan using
//     audit_log_tenant_at_idx. The second result is the proof the first left
//     nothing behind -- pg_statistic is transactional. audit_log is append-only
//     (section 4.3), so a probe that COMMITTED its fixture would add rows no role
//     can ever remove, which is backlog T52.
//
// Measured floor for the volume: 5 rows is already enough on a freshly migrated
// database and 0 is enough on the 194 770-row development one. 200 is used, well
// clear of the floor and free, since none of it is kept.
//
// ⚠️ THE EXPLAIN ITSELF RUNS AS tappa_app, VIA SET LOCAL ROLE, so RLS is in force
// and the plan carries the policy's One-Time Filter -- an index that only worked
// with RLS off would be no use. The honest limit: SET LOCAL ROLE exercises the
// POLICY but not pg_hba or the credential, unlike a real tappa_app connection.
// For a question about plan shape that difference does not exist (the policy is
// applied identically); for anything about authentication it would, and this test
// makes no such claim. TestRLS_AuditTargetIndex_DoesNotLeakAcrossTenants below
// asks the isolation question over a genuine tappa_app pool.
func TestAuditIndex00021_PlaqueHistoryUsesTheIndex(t *testing.T) {
	owner := ownerDB(t)

	tenantID := uuid.New()
	target := "AUDITIDX" + strings.ToUpper(uuid.New().String()[:6])

	// Returned by the closure so WithTenant rolls back instead of committing.
	// WithTenant commits when fn returns nil; there is no other exit.
	errRollback := errors.New("audit index probe finished -- roll the fixture back")

	var plan, probeRole string
	err := owner.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'audit-index', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		// 200 rows, all in this tenant, all with DISTINCT targets -- distinct is
		// what makes `target = $2` selective and therefore what the planner is
		// being asked about. One of them is the target the probe then looks up.
		if _, e := tx.Exec(ctx,
			`INSERT INTO audit_log (tenant_id, action, target, detail)
			 SELECT $1, 'plaque.mounted', $2 || g::text, '{}'::jsonb
			   FROM generate_series(1, 199) g`,
			tenantID, target); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO audit_log (tenant_id, action, target, detail)
			 VALUES ($1, 'plaque.mounted', $2, '{}'::jsonb)`,
			tenantID, target); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `ANALYZE audit_log`); e != nil {
			return fmt.Errorf("ANALYZE audit_log (needs the table owner): %w", e)
		}

		// From here on the session is the application's role, so the policy
		// applies to the EXPLAIN below.
		if _, e := tx.Exec(ctx, `SET LOCAL ROLE tappa_app`); e != nil {
			return e
		}
		// Proven, not assumed: a superuser or BYPASSRLS role would silently make
		// the "RLS in force" claim above false.
		var super, bypass bool
		if e := tx.QueryRow(ctx,
			`SELECT current_user, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`,
		).Scan(&probeRole, &super, &bypass); e != nil {
			return e
		}
		if probeRole != "tappa_app" || super || bypass {
			return fmt.Errorf("probe runs as %q (super=%v bypassrls=%v), want tappa_app with neither",
				probeRole, super, bypass)
		}

		rows, e := tx.Query(ctx,
			`EXPLAIN SELECT a.action, a.at
			   FROM audit_log a
			  WHERE a.tenant_id = $1
			    AND a.target = $2::text
			    AND a.action LIKE 'plaque.%'
			  ORDER BY a.at DESC
			  LIMIT 50`, tenantID, target)
		if e != nil {
			return e
		}
		defer rows.Close()
		var b strings.Builder
		for rows.Next() {
			var line string
			if e := rows.Scan(&line); e != nil {
				return e
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
		if e := rows.Err(); e != nil {
			return e
		}
		plan = b.String()
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("audit index probe: %v", err)
	}

	// 🔴 THE ROLLBACK IS ASSERTED, NOT ASSUMED, and it is checked from a SEPARATE
	// connection so the answer is about committed state. audit_log is append-only
	// (section 4.3): no role in this system can delete a row from it, so a probe
	// that committed its 200-row fixture would leave 200 permanent rows -- backlog
	// T52 is precisely that residue. `return errRollback` is one keyword away from
	// `return nil`, and `return nil` would be silent. This makes it loud.
	var leftBehind int
	if e := appDB(t).WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, tenantID).Scan(&leftBehind)
	}); e != nil {
		t.Fatalf("check that the probe rolled back: %v", e)
	}
	if leftBehind != 0 {
		t.Fatalf("the probe COMMITTED %d audit rows for tenant %s. audit_log is append-only, so they could "+
			"never be removed again (backlog T52) -- the probe transaction must roll back", leftBehind, tenantID)
	}

	if !strings.Contains(plan, auditTargetIndex) {
		t.Fatalf("ListPlaqueHistory's predicate does NOT reach the rows through %s, even with 200 rows in "+
			"the tenant and fresh statistics. Check WHICH path the planner took below: another index name "+
			"means the index lost a cost race, no index name at all means it is gone or unusable (backlog "+
			"T17). Plan:\n%s", auditTargetIndex, plan)
	}

	// 🔴 THE PROPERTY IS `target` IN AN Index Cond, NOT THE ABSENCE OF A BITMAP
	// NODE -- AND THAT IS A CORRECTION THIS TEST EARNED THE HARD WAY. The previous
	// version also failed the plan whenever it contained "Bitmap Heap Scan on
	// audit_log". MEASURED on a freshly migrated database: the planner produced a
	// Bitmap Index Scan on audit_log_tenant_target_idx with
	// `Index Cond: (tenant_id = ... AND target = ...)` feeding a Bitmap Heap Scan
	// -- an entirely index-driven plan, and on a two-page table the CHEAPEST one --
	// and the old assertion called it a regression. Bitmap vs plain index scan is
	// the planner's business and it changes with table size; it was never the
	// point.
	//
	// The point is WHERE `target` is evaluated, which is exactly what separates the
	// BEFORE and AFTER measurements in migration 00021:
	//   BEFORE  Index/Bitmap on (tenant_id, at) + `Filter: (target = ...)`
	//           -> every audit row the TENANT owns is fetched and then discarded
	//              (Rows Removed by Filter: 3 688)
	//   AFTER   `Index Cond: (tenant_id = ... AND target = ...)`
	//           -> target is a SEEK key, so the rows are never fetched at all
	// A plan that filters on target has not solved T17 no matter which node types
	// it uses.
	seeksOnTarget := false
	for _, line := range strings.Split(plan, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Index Cond:") && strings.Contains(trimmed, "target") {
			seeksOnTarget = true
		}
		if strings.HasPrefix(trimmed, "Filter:") && strings.Contains(trimmed, "target") {
			t.Fatalf("the plan FILTERS on target instead of seeking on it, which is the pre-00021 behaviour "+
				"T17 exists to remove: every audit row the tenant owns is fetched and thrown away. Plan:\n%s", plan)
		}
	}
	if !seeksOnTarget {
		t.Fatalf("no Index Cond in the plan mentions target, so %s is being used for tenant_id alone and "+
			"the second column is buying nothing (backlog T17). Plan:\n%s", auditTargetIndex, plan)
	}
	// The policy has to be visible in the plan, otherwise "measured with RLS in
	// force" is a claim about a session that was not actually subject to it.
	if !strings.Contains(plan, "app.tenant_id") {
		t.Fatalf("the plan carries no app.tenant_id filter, so RLS was NOT in force for this probe "+
			"(role was %q) and the measurement does not describe production. Plan:\n%s", probeRole, plan)
	}
}

// TestRLS_AuditTargetIndex_DoesNotLeakAcrossTenants is the isolation proof the
// new index owes section 4.5, and it is deliberately shaped UNLIKE a production
// query.
//
// 🔴 NO `tenant_id =` PREDICATE. CLAUDE.md section 6 says the two shapes are
// different jobs: a production query MUST carry the explicit filter (belt over
// RLS's braces), and an isolation test MUST NOT -- with the filter present, zero
// rows would prove the WHERE worked, and the test would stay green with RLS
// switched off. The index makes this worth restating: an index is a second
// physical path to the same rows, and the question "does the new path honour the
// policy" can only be asked by a query that has nothing else stopping it.
//
// Both tenants write a row with the SAME target, so tenant B's probe would return
// A's row if the policy were bypassed -- and B's own row proves the query, the
// index and the target string are all working (a probe that returns nothing for
// everybody proves nothing).
func TestRLS_AuditTargetIndex_DoesNotLeakAcrossTenants(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	tenantA, tenantB := uuid.New(), uuid.New()
	// One shared subject string: the plaque uid a manager would open a card for.
	target := "AUDITIDX" + strings.ToUpper(uuid.New().String()[:6])
	seedAuditRows(t, app, tenantA, target)
	seedAuditRows(t, app, tenantB, target)

	// A's row is identified by its ACTION, not by its tenant, so a 0 below is RLS.
	countByTarget := func(scope uuid.UUID, action string) int {
		t.Helper()
		var n int
		if err := app.WithTenant(context.Background(), scope, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM audit_log WHERE target = $1::text AND action = $2`,
				target, action).Scan(&n)
		}); err != nil {
			t.Fatalf("count in %s: %v", scope, err)
		}
		return n
	}

	if got := countByTarget(tenantA, "plaque.mounted."+tenantA.String()); got != 1 {
		t.Fatalf("tenant A sees %d of its own rows, want 1 (positive control: the probe works)", got)
	}
	if got := countByTarget(tenantB, "plaque.mounted."+tenantA.String()); got != 0 {
		t.Fatalf("tenant B sees %d of tenant A's audit rows through target %q -- "+
			"audit_log_tenant_target_idx must not become a cross-tenant read path", got, target)
	}
	if got := countByTarget(tenantA, "plaque.mounted."+tenantB.String()); got != 0 {
		t.Fatalf("tenant A sees %d of tenant B's audit rows, want 0", got)
	}
}

// seedAuditRows commits a tenant and one plaque audit row against `target`, as
// tappa_app inside that tenant's context. The action carries the tenant id so a
// cross-tenant read is identifiable without asking for tenant_id in the WHERE.
func seedAuditRows(t *testing.T, d *DB, tenantID uuid.UUID, target string) {
	t.Helper()
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'audit-index', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO audit_log (tenant_id, action, target, detail)
			 VALUES ($1, $2, $3, '{}'::jsonb)`,
			tenantID, "plaque.mounted."+tenantID.String(), target)
		return e
	}); err != nil {
		t.Fatalf("seedAuditRows(%s): %v", tenantID, err)
	}
}
