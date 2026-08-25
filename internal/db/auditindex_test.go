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
// 🔴 THE PAIR "3 688 OF 193 064" IS DATED AND CAN NO LONGER BE REPRODUCED, and it
// is labelled rather than quietly left to rot. Re-checked 2026-08-25: the tenant
// that probe named (10000000-...-0001) now holds ZERO audit rows -- the development
// database has been refreshed since -- so anyone re-running it gets nothing and
// could reasonably conclude the number was invented. It was not; it is history,
// and this file's rule is that a number is either wired to a gate, DATED, or
// deleted. This one is dated.
//
// RE-MEASURED 2026-08-25 ON THE SAME DEVELOPMENT DATABASE, same probe shape, with
// the index DROPPED inside a rolled-back transaction so BEFORE is the real absence
// of it rather than a planner flag. Tenant 10000000-0000-4000-8000-000000000001,
// 5 574 audit rows of 279 436:
//
//	BEFORE  Bitmap Heap Scan, Rows Removed by Filter: 5 573,
//	        Heap Blocks: exact=1 787, Buffers: shared hit=1 838
//	        6.216 / 5.288 / 6.216 ms
//	AFTER   Index Scan using audit_log_tenant_target_idx,
//	        Buffers: shared hit=7
//	        0.161 / 0.253 / 0.146 ms
//
// Same shape, same two orders of magnitude, three months of drift later: every row
// the TENANT owns is fetched and discarded without the index, and 7 buffers with
// it. The 2026-08-19 figures above are superseded as row counts and confirmed as a
// claim.
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
//   - USABILITY (CAN the planner reach the rows through it, with target as a
//     SEEK KEY) is what TestAuditIndex00021_PlaqueHistoryCanSeekOnTheIndex
//     asserts. It is NOT the same question as "will the planner CHOOSE it", and
//     🔴 THAT SECOND QUESTION IS NO LONGER ASSERTED ANYWHERE IN THIS FILE. This
//     sentence used to read "USABILITY (will the planner actually choose it) is
//     a COST fact" and it is corrected here rather than only in the body,
//     because a reader who starts at the TOP would otherwise leave believing CI
//     still defends a planner PREFERENCE. It does not, deliberately -- the full
//     reasoning, the measurements and the honest statement of what was given up
//     are on that test below, and this is the third round in which the fix and
//     the surrounding prose had to be made to agree.
//
//     Choice is a COST fact: cost depends on statistics, statistics depend on
//     data, and CI's data is not production's. Two rounds tried to settle it
//     with a bigger fixture and CI falsified both. Measured on a freshly
//     migrated database, running the then-current test verbatim -- ⚠️ AND NOTE
//     WHICH AXIS THIS IS: rows in the TABLE, while the probe's own tenant held
//     exactly ONE. The table further down on that test counts rows in the
//     TENANT, on an otherwise empty audit_log. Different magnitudes, not a
//     contradiction:
//
//     rows in the TABLE (tenant holds 1)   101    122    223    424   1 425
//     verdict                              FAIL   FAIL   FAIL   FAIL  FAIL
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

// TestAuditIndex00021_PlaqueHistoryCanSeekOnTheIndex runs ListPlaqueHistory's
// exact predicate under EXPLAIN with sequential scans disabled, and requires the
// planner to reach the rows through the index WITH target as a seek key -- under
// conditions it builds itself instead of hoping the database it is pointed at
// happens to have them.
//
// 🔴 "CAN SEEK", NOT "USES" -- THE QUESTION WAS DELIBERATELY NARROWED, AND CI
// FALSIFIED THE OLD ONE TWICE. Runs 32570619152 (2026-08-22) and 32856990423
// (2026-08-25) failed identically on `Seq Scan on audit_log a (cost=0.01..6.62)`,
// and THE PLANNER WAS RIGHT: on CI's table that scan is cheaper than the index
// path, which costs a flat 8.30..8.31 (0.29 startup + two random page fetches at
// random_page_cost = 4). Nothing was broken; the test was asserting a cost
// outcome under conditions in which the outcome it demanded was the wrong one.
//
// ⚠️ AND THE HEADER ABOVE HAD ALREADY DIAGNOSED THIS, IN THESE WORDS: "the old
// test did not have a threshold problem; it was asking a cost question under
// conditions in which the answer it demanded would have been wrong." The diagnosis
// was right and the remedy did not follow it -- the answer was a bigger fixture,
// which is a threshold. So this is the same mistake twice, and the second time it
// was made on top of its own correct post-mortem. That is the reason the fix below
// changes the QUESTION rather than the number: a number is what the last two
// rounds both reached for.
//
// The paragraph that used to stand here said: "5 rows is already enough on a
// freshly migrated database ... 200 is used, well clear of the floor." THAT
// MEASUREMENT WAS WRONG, and re-measuring it showed why: probes that ROLL BACK
// still leave dead heap pages behind, so a sweep run in one sitting inflates the
// table under its own feet and every later N looks better than it is. With
// `VACUUM FULL audit_log` before each trial (postgres 17.10, compose defaults,
// fresh schema, 2026-08-25) the real curve is:
//
//	rows in the TENANT     5      50    100    200    240    260    400   1000+
//	seq scan cost        1.11   1.90   3.77   6.52   8.22      -      -       -
//	planner picks         SEQ    SEQ    SEQ    SEQ    SEQ    IDX    IDX     IDX
//
// (a dash means the chosen plan had no Seq Scan node to quote a cost from.)
//
// ⚠️ THE AXIS IS ROWS IN THE TENANT, ON AN OTHERWISE EMPTY audit_log -- so here
// tenant rows and table rows are the same number. The header's table above counts
// rows in the TABLE while its tenant held exactly ONE. Both are correct and they
// are not the same measurement; read the axis before comparing them.
//
// The crossover is ~250 and 200 sat 20% BELOW it, not "well clear" above it. CI
// was never near a floor; it was on the wrong side of a knife edge. And it had no
// help from anywhere else: test/fixtures/seed.sql writes NO audit_log rows (it
// says so in its own header), so audit_log in CI holds only what the suite itself
// commits -- a handful of rows, and since `go test ./...` runs packages in
// PARALLEL that handful is a race. CI's 6.62 against this machine's 6.52 for the
// same 200-row fixture IS that handful.
//
// 🔴 AND NO OTHER NUMBER WOULD HAVE BEEN SAFE EITHER -- THE THRESHOLD IS NOT THE
// TEST'S TO PICK. The crossover moves with cost GUCs the test does not control.
// Measured on the same fresh schema, fixture rebuilt on a vacuumed table each
// time, over the ladder 200 / 400 / 1 000 / 2 000 / 5 000 / 10 000 / 20 000 --
// smallest rung that wins:
//
//	random_page_cost    1.1     4      10      20      40
//	smallest rung       200    400   1 000   2 000   5 000
//
// 4 is the compose default; 1.1 is what almost every managed Postgres on SSD
// ships. That is a 25x spread across two ordinary settings -- not a floor to
// clear, a coin to flip -- and Postgres version, row width, page density and
// parallel plan thresholds move it further. So the probe asks the decidable
// question instead:
// `SET LOCAL enable_seqscan = off` takes the sequential scan off the table and
// asks whether the index is USABLE for this predicate. Measured stable across
// random_page_cost 0.5, 1.1, 2, 4, 10, 20, 40 and 100: Index Scan using
// audit_log_tenant_target_idx with `Index Cond: (tenant_id = ... AND target = ...)`
// in all eight, on both a fresh schema and this machine's 278 004-row one.
//
// 🔴 WHAT THIS TEST NO LONGER SAYS, BY NAME. It does NOT prove the planner would
// CHOOSE this index over a sequential scan at any volume, and least of all at
// production volume -- which is exactly where T17's fear ("ListPlaqueHistory scans
// the tenant's whole audit history") actually lives. That is a real narrowing and
// it should not be dressed up.
//
// What it is NOT, though, is the loss of a production guarantee, because the old
// assertion never carried one: it asserted a preference over a 200-ROW FIXTURE,
// and 200 rows is not the volume T17 is about. It bought a claim about a toy table
// at the price of a threshold that could never be made durable. The production
// claim is evidence, not an assertion, and it is recorded where evidence belongs
// -- the BEFORE/AFTER measurement in migration 00021's header and at the top of
// this file (3 688 rows of 193 064: 1 184 heap blocks -> 3 buffers, 2.0 ms ->
// 0.16 ms). ⚠️ That row-count pair is DATED and no longer reproducible; the header
// says so and gives the 2026-08-25 equivalent that is. What CI can still decide,
// it still decides: the index EXISTS
// (TestAuditIndex00021_IndexShape), has the right columns in the right order
// (ditto), honours RLS (TestRLS_AuditTargetIndex_DoesNotLeakAcrossTenants), and is
// USABLE for this predicate with target as a SEEK KEY rather than a filter (here).
// An index with those four properties is one the planner reaches for as soon as
// the volume makes it worth reaching for.
//
// 🔴 THE WHOLE PROBE IS ONE TRANSACTION THAT IS ROLLED BACK, AND EVERY PART OF
// THAT SENTENCE IS LOAD-BEARING:
//
//   - it INSERTS a tenant holding several DISTINCT targets, because that is what
//     makes `target = $2` selective and therefore what makes the second index
//     column worth anything (measured: with the tenant holding 0 extra rows the
//     planner picks audit_log_tenant_at_idx even with seqscan off, and it is right
//     to -- on a one-row tenant the two paths are a tie);
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
// 🔴 THE ANALYZE IS STILL LOAD-BEARING AND WAS RE-MEASURED, because disabling the
// sequential scan removes only ONE of the two jobs the fixture used to do. Without
// ANALYZE, at every volume tried (0, 1, 5 and 200 extra rows, seqscan off), the
// plan is `Index Scan using audit_log_tenant_at_idx` with `Filter: (target = ...)`
// -- the pre-00021 shape -- because reltuples is 0 and the planner has no reason to
// believe target narrows anything. The fixture no longer has to OUTWEIGH a seq
// scan; it still has to make target look SELECTIVE.
//
// Re-measured floor under the new question -- this floor is about STATISTICS, not
// page count, which is why it is two orders of magnitude below the old one.
//
// 🔴 BUT THE FLOOR IS NOT A CONSTANT: IT CLIMBS WITH THE ROWS ALREADY COMMITTED IN
// audit_log, because rows belonging to OTHER tenants dilute what ANALYZE learns
// about how selective `target = $2` is. Stating the floor without stating that
// condition is how the last two rounds went wrong, so here is the condition.
// Measured on a freshly migrated schema, VACUUM FULL before every trial, 3 trials
// per cell (2026-08-25):
//
//	rows already committed    extra=0   extra=1   extra=2   extra=5
//	0 (empty audit_log)       FILTER    seek      seek      seek
//	2 (other tenants)         FILTER    FILTER    seek      seek
//	4 (other tenants)         FILTER    FILTER    FILTER    seek
//
// ⚠️ AND THE 2-ROW LINE IS THE ONE CI ACTUALLY SEES, because those rows are not
// hypothetical: TestRLS_AuditTargetIndex_DoesNotLeakAcrossTenants, at the bottom of
// THIS FILE, commits exactly 2 audit rows on every run (one per tenant, both
// permanent -- audit_log is append-only). So the floor under CI is at least 2, not
// 1, and it rises a little with every run the database has ever seen.
//
// 200 is kept: it is two orders of magnitude above any floor in that table, which
// is the margin the previous two rounds did not have. It also gives ANALYZE a
// sample worth taking, and it is free -- the whole probe (fixture, ANALYZE,
// EXPLAIN, rollback) measured 0.11 s against a CI-sized database, and none of it
// is kept.
//
// ⚠️ THE EXPLAIN ITSELF RUNS AS tappa_app, VIA SET LOCAL ROLE, so RLS is in force
// and the plan carries the policy's One-Time Filter -- an index that only worked
// with RLS off would be no use. The honest limit: SET LOCAL ROLE exercises the
// POLICY but not pg_hba or the credential, unlike a real tappa_app connection.
// For a question about plan shape that difference does not exist (the policy is
// applied identically); for anything about authentication it would, and this test
// makes no such claim. TestRLS_AuditTargetIndex_DoesNotLeakAcrossTenants below
// asks the isolation question over a genuine tappa_app pool.
func TestAuditIndex00021_PlaqueHistoryCanSeekOnTheIndex(t *testing.T) {
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

		// 🔴 THIS LINE IS THE FIX FOR TWO RED CI RUNS, and the doc comment above
		// carries the measurement. It takes the sequential scan off the table so
		// the question becomes "is the index USABLE for this predicate", which the
		// test controls, instead of "is the index CHEAPER than reading the whole
		// table", which depends on volume and cost GUCs that it does not.
		// SET LOCAL, so it dies with the transaction like everything else here.
		if _, e := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); e != nil {
			return e
		}

		// From here on the session is the application's role, so the policy
		// applies to the EXPLAIN below.
		if _, e := tx.Exec(ctx, `SET LOCAL ROLE tappa_app`); e != nil {
			return e
		}
		// Proven, not assumed -- the same reason the role is proven below. If a
		// refactor ever moved the SET LOCAL onto a different connection or outside
		// the transaction, the probe would quietly become the volume-dependent test
		// that failed CI twice, and it would be green on the machine that wrote it.
		var seqscan string
		if e := tx.QueryRow(ctx, `SHOW enable_seqscan`).Scan(&seqscan); e != nil {
			return e
		}
		if seqscan != "off" {
			return fmt.Errorf("enable_seqscan is %q inside the probe, want off", seqscan)
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
		t.Fatalf("ListPlaqueHistory's predicate does NOT reach the rows through %s, with 200 rows in the "+
			"tenant, fresh statistics AND sequential scans disabled -- so this is not a cost race the index "+
			"lost, it is the index being unusable for this predicate (backlog T17). Check WHICH path the "+
			"planner took below: audit_log_tenant_at_idx means it fell back to the pre-00021 path, no index "+
			"name at all means the index is gone. Plan:\n%s", auditTargetIndex, plan)
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
