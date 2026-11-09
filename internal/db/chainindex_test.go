package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// chainindex_test.go -- T157, migrations 00091, 00092 and 00094: the anomaly lists' per-row chain
// lookups read the three partial indexes, so neither a person's directionless taps (refused, ignored --
// type NULL) nor a run of their checkouts is walked once per check-in or checkout.
//
// MEASURED ON THE STATEMENTS' OWN DATA, NOT ON THE SHARED TABLE (00059's plan pin and its reason,
// webhookretention_test.go): the shipped statements are planned against a temporary copy of
// transactions -- LIKE ... INCLUDING INDEXES, so the copy carries a copy of every index, the two
// partial ones included -- filled with a business whose one person has a flood of directionless rows
// inside the week, and ANALYZEd. Whatever other packages' tests leave in the real table does not move
// the plan. The copy is dropped with the transaction, which is always rolled back.
//
// AND PLANNED AS THE APPLICATION PLANS IT (security lens, T157 round): the copy carries transactions'
// own row-security policy (its expression read from the catalogue, ENABLE + FORCE), and every statement
// is planned twice -- as the owner, whom RLS does not bind, and as tappa_app in the business's context,
// the plan production runs. A later policy that stopped the tenant predicate reaching the index would
// turn the second one red.

// chainCopy is the temporary table's name; the shipped statements' "transactions <alias>" are
// pointed at it.
const chainCopy = "t157_chain_copy"

// chainIndexCopy is the name the copy's index got for the real index `real` -- the one whose columns
// and predicate are the real one's.
func chainIndexCopy(t *testing.T, ctx context.Context, tx pgx.Tx, real string) string {
	t.Helper()
	var name string
	if err := tx.QueryRow(ctx, `
		SELECT coalesce((SELECT string_agg(c.indexrelid::regclass::text, ',')
		                   FROM pg_index AS c, pg_index AS r
		                  WHERE c.indrelid = $1::regclass
		                    AND r.indexrelid = to_regclass($2)
		                    AND c.indkey::text = r.indkey::text
		                    AND pg_get_expr(c.indpred, c.indrelid) IS NOT DISTINCT FROM pg_get_expr(r.indpred, r.indrelid)), '')`,
		chainCopy, "public."+real).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name == "" || strings.Contains(name, ",") {
		t.Fatalf("the copy of transactions has %q for %s, want exactly one index", name, real)
	}
	return name
}

// TestChainLookups_ReadTheirPartialIndexes plans ListOpenCheckIns, CountMaskedOpenCheckIns and
// ListCheckoutsWithoutCheckIn -- the text sqlc generated -- in the generic plan pgx's statement cache
// settles on and in a custom one, as the owner and as tappa_app, and holds every per-row lookup to its
// index with no row filtered: the next / previous record (n, pv) to 00091's, the latest `in` (i) to
// 00094's, "a practice row after it" (p) to 00092's.
func TestChainLookups_ReadTheirPartialIndexes(t *testing.T) {
	owner := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tx, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if rb := tx.Rollback(context.Background()); rb != nil && !errors.Is(rb, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", rb)
		}
	}()

	// One business, 20 people, 60 days of 09:00 in / 17:00 out (every seventh day's out missing, every
	// eleventh day's in), a flood of 20 000 directionless rows of the first person's inside the last
	// week, a run of 2 000 consecutive checkouts of the second person's in it (ORTA-1's shape), and 20
	// historical practice rows long before it.
	for _, s := range []string{
		`CREATE TEMPORARY TABLE ` + chainCopy + ` (LIKE public.transactions INCLUDING DEFAULTS INCLUDING INDEXES) ON COMMIT DROP`,
		`CREATE TEMPORARY TABLE t157_people ON COMMIT DROP AS
		   SELECT gen_random_uuid() AS id, row_number() OVER () AS n FROM generate_series(1, 20)`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel, business_day_start)
		 SELECT '00000000-0000-4000-8000-00000000f157', p.id, x.type,
		        date_trunc('day', now()) - make_interval(days => d) + x.at, 'ok', 'qr',
		        date_trunc('day', now()) - make_interval(days => d)
		   FROM t157_people AS p, generate_series(0, 59) AS d,
		        (VALUES ('in', interval '9 hours'), ('out', interval '17 hours')) AS x(type, at)
		  WHERE NOT (x.type = 'out' AND d % 7 = 3) AND NOT (x.type = 'in' AND d % 11 = 5)`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel)
		 SELECT '00000000-0000-4000-8000-00000000f157', p.id, NULL,
		        date_trunc('day', now()) - interval '3 days' + interval '9 hours' + g * interval '1 second',
		        CASE WHEN g % 2 = 0 THEN 'ignored' ELSE 'reject' END, 'qr'
		   FROM t157_people AS p, generate_series(1, 20000) AS g WHERE p.n = 1`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel, business_day_start)
		 SELECT '00000000-0000-4000-8000-00000000f157', p.id, 'out',
		        date_trunc('day', now()) - interval '2 days' + interval '17 hours' + g * interval '1 minute', 'ok', 'qr',
		        date_trunc('day', now()) - interval '2 days'
		   FROM t157_people AS p, generate_series(1, 2000) AS g WHERE p.n = 2`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel, practice)
		 SELECT '00000000-0000-4000-8000-00000000f157', p.id, 'in', now() - interval '400 days', 'ok', 'qr', true
		   FROM t157_people AS p`,
		`ANALYZE ` + chainCopy,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	directed := chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_directed_idx")
	practice := chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_practice_idx")
	checkin := chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_checkin_idx")

	// transactions' policy on the copy, its two expressions as the catalogue holds them.
	var using, check string
	if err := tx.QueryRow(ctx, `SELECT pg_get_expr(polqual, polrelid), pg_get_expr(polwithcheck, polrelid)
	                              FROM pg_policy WHERE polrelid = 'public.transactions'::regclass
	                               AND polname = 'transactions_tenant_isolation'`).Scan(&using, &check); err != nil {
		t.Fatalf("read transactions' policy: %v", err)
	}
	if !strings.Contains(using, "current_setting('app.tenant_id'") {
		t.Fatalf("PREMISE: transactions' policy reads %q", using)
	}
	for _, s := range []string{
		`ALTER TABLE ` + chainCopy + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + chainCopy + ` FORCE ROW LEVEL SECURITY`,
		`CREATE POLICY t157_tenant_isolation ON ` + chainCopy + ` USING (` + using + `) WITH CHECK (` + check + `)`,
		`GRANT SELECT ON ` + chainCopy + ` TO tappa_app`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}

	// alias -> the index its scans must use.
	cases := []struct {
		name, constName string
		args            string
		lookups         map[string]string
	}{
		{"ListOpenCheckIns", "listOpenCheckIns", "$1, $2, $3, 51",
			map[string]string{"n": directed, "p": practice}},
		{"CountMaskedOpenCheckIns", "countMaskedOpenCheckIns", "$1, $2, $3",
			map[string]string{"n": directed, "p": practice}},
		{"ListCheckoutsWithoutCheckIn", "listCheckoutsWithoutCheckIn", "$1, $2, $3, 51",
			map[string]string{"pv": directed, "i": checkin}},
	}
	alias := regexp.MustCompile(`\btransactions ([a-z]+)\b`)
	scan := regexp.MustCompile(`^\s*(?:->\s+)?((?:Parallel )?(?:Index Only Scan Backward|Index Only Scan|Index Scan Backward|Index Scan|Bitmap Heap Scan|Seq Scan))(?: using (\S+))? on ` + chainCopy + ` ([a-z]+)(?:_\d+)?\b`)
	for _, tc := range cases {
		stmt := shippedStoreSQL(t, "anomalies.sql.go", tc.constName)
		if !strings.Contains(stmt, "FROM transactions ") {
			t.Fatalf("PREMISE: %s reads no `transactions <alias>`", tc.name)
		}
		stmt = alias.ReplaceAllString(stmt, chainCopy+" $1")
		for _, run := range []struct{ role, mode string }{
			{"owner", "force_generic_plan"}, {"owner", "force_custom_plan"},
			{"tappa_app", "force_generic_plan"}, {"tappa_app", "force_custom_plan"},
		} {
			t.Run(tc.name+"/"+run.role+"/"+run.mode, func(t *testing.T) {
				name := "t157_" + strings.ToLower(tc.name)
				setup := []string{`RESET ROLE`, `SET LOCAL plan_cache_mode = ` + run.mode}
				if run.role == "tappa_app" {
					setup = append(setup, `SET LOCAL ROLE tappa_app`,
						`SELECT set_config('app.tenant_id', '00000000-0000-4000-8000-00000000f157', true)`)
				}
				t.Cleanup(func() {
					if _, err := tx.Exec(ctx, `RESET ROLE`); err != nil {
						t.Errorf("reset role: %v", err)
					}
				})
				for _, s := range append(setup, `PREPARE `+name+` AS `+stmt) {
					if _, err := tx.Exec(ctx, s); err != nil {
						t.Fatalf("%s: %v", s, err)
					}
				}
				q := fmt.Sprintf(`EXPLAIN (COSTS OFF) EXECUTE %s('00000000-0000-4000-8000-00000000f157', date_trunc('day', now()) - interval '7 days', now()%s)`,
					name, strings.TrimPrefix(tc.args, "$1, $2, $3"))
				rows, err := tx.Query(ctx, q)
				if err != nil {
					t.Fatalf("%s: %v", q, err)
				}
				plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `DEALLOCATE `+name); err != nil {
					t.Fatal(err)
				}
				text := strings.Join(plan, "\n")
				seen := map[string]int{}
				for i, line := range plan {
					m := scan.FindStringSubmatch(line)
					if m == nil {
						continue
					}
					want, ok := tc.lookups[m[3]]
					if !ok {
						continue // the outer scan of the week -- backlog T244, not pinned here
					}
					seen[m[3]]++
					if m[2] != want {
						t.Errorf("%s reads %s with %s %s, want %s:\n%s", tc.name, m[3], m[1], m[2], want, text)
					}
					// No row of the chain is filtered at the scan: every condition is the index's. The one
					// exception is the policy's own predicate standing alone (chainPolicyOnly).
					for _, next := range plan[i+1:] {
						if strings.Contains(next, "->") {
							break
						}
						if f := strings.TrimSpace(next); strings.Contains(f, "Filter:") && !chainPolicyOnly(f) {
							t.Errorf("%s/%s filters rows of %s on %s: %q", run.role, run.mode, m[3], m[2], f)
						}
					}
					// As tappa_app the probe is the policy's: its predicate guards THIS scan -- on the
					// Result that wraps it (a One-Time Filter, the shape the planner gives a tenant
					// predicate it can prove from tenant_id = $1) or on the scan itself.
					if run.role == "tappa_app" && !chainGuarded(plan, i) {
						t.Errorf("PREMISE: tappa_app's scan of %s carries no row-security predicate:\n%s", m[3], text)
					}
				}
				for a := range tc.lookups {
					if seen[a] == 0 {
						t.Errorf("PREMISE: %s's plan has no scan of %s:\n%s", tc.name, a, text)
					}
				}
			})
		}
	}
}

// chainPolicy is transactions' policy expression as EXPLAIN prints it on a scan.
const chainPolicy = "(NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid"

// chainPolicyOnly reports whether a plan's filter line is the row-security predicate and nothing
// else: a One-Time Filter (evaluated once, it filters no row of the scan) on the policy, or a Filter
// whose whole expression is `tenant_id = <policy>` either way round. A Filter that also carries
// anything else -- a row comparison that fell out of the Index Cond, a type or practice test -- is not.
func chainPolicyOnly(line string) bool {
	if rest, ok := strings.CutPrefix(line, "One-Time Filter: "); ok {
		return strings.Contains(rest, chainPolicy) && !strings.Contains(rest, " AND ") && !strings.Contains(rest, " OR ")
	}
	rest, ok := strings.CutPrefix(line, "Filter: ")
	if !ok {
		return false
	}
	rest = strings.TrimSuffix(strings.TrimPrefix(rest, "("), ")")
	return rest == "tenant_id = "+chainPolicy || rest == chainPolicy+" = tenant_id"
}

// chainGuarded reports whether the scan at plan[i] is under the COPY's row-security predicate: on
// its own detail lines, or on the nearest node above it that carries one -- and then only if every
// relation scanned below that node is the copy, so that employees' or locations' policy (the same
// text once tenant_id = $1 is folded in) cannot stand in for it.
func chainGuarded(plan []string, i int) bool {
	for _, next := range plan[i+1:] {
		if strings.Contains(next, "->") {
			break
		}
		if strings.Contains(next, chainPolicy) {
			return true
		}
	}
	col := chainCol(plan[i])
	for j := i - 1; j >= 0; j-- {
		c := chainCol(plan[j])
		if !strings.Contains(plan[j], "->") && j > 0 {
			continue // a detail line; it is read with its node below
		}
		if c >= col {
			continue // a sibling's subtree, not an ancestor
		}
		col = c
		// plan[j] is an ancestor: its own line and its detail lines.
		guarded := strings.Contains(plan[j], chainPolicy)
		for _, d := range plan[j+1:] {
			if strings.Contains(d, "->") {
				break
			}
			guarded = guarded || strings.Contains(d, chainPolicy)
		}
		if !guarded {
			continue
		}
		for _, d := range plan[j+1:] {
			if strings.Contains(d, "->") && chainCol(d) <= c && j > 0 {
				break
			}
			// A relation scan (a Bitmap Index Scan names an index, not a relation).
			if strings.Contains(d, " Scan") && strings.Contains(d, " on ") && !strings.Contains(d, "Bitmap Index Scan") &&
				!strings.Contains(d, " on "+chainCopy+" ") {
				return false
			}
		}
		return true
	}
	return false
}

// chainCol is the column a plan line's node arrow stands at (the root has none: -1).
func chainCol(line string) int {
	return strings.Index(line, "->")
}
