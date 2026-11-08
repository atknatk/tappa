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

// chainindex_test.go -- T157, migrations 00091 and 00092: the anomaly lists' per-row chain lookups
// read the two partial indexes, so a person's directionless taps (refused, ignored -- type NULL) are
// not walked once per check-in or checkout.
//
// MEASURED ON THE STATEMENTS' OWN DATA, NOT ON THE SHARED TABLE (00059's plan pin and its reason,
// webhookretention_test.go): the shipped statements are planned against a temporary copy of
// transactions -- LIKE ... INCLUDING INDEXES, so the copy carries a copy of every index, the two
// partial ones included -- filled with a business whose one person has a flood of directionless rows
// inside the week, and ANALYZEd. Whatever other packages' tests leave in the real table does not move
// the plan. The copy is dropped with the transaction, which is always rolled back.

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

// TestChainLookups_ReadTheDirectedAndThePracticeIndexes plans ListOpenCheckIns,
// CountMaskedOpenCheckIns and ListCheckoutsWithoutCheckIn -- the text sqlc generated -- in the generic
// plan pgx's statement cache settles on and in a custom one, and holds every per-row lookup to its
// index: the next / previous record (n, pv) and the latest `in` (i) to 00091's, with no row filtered
// on n and pv; "a practice row after it" (p) to 00092's.
func TestChainLookups_ReadTheDirectedAndThePracticeIndexes(t *testing.T) {
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
	// week, and 20 historical practice rows long before it.
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

	// alias -> the index its scans must use, and whether a Filter line may follow (i walks the
	// person's checkouts back to the latest `in`, so its type test is a filter by design).
	type lookup struct {
		index     string
		mayFilter bool
	}
	cases := []struct {
		name, constName string
		args            string
		lookups         map[string]lookup
	}{
		{"ListOpenCheckIns", "listOpenCheckIns", "$1, $2, $3, 51",
			map[string]lookup{"n": {directed, false}, "p": {practice, false}}},
		{"CountMaskedOpenCheckIns", "countMaskedOpenCheckIns", "$1, $2, $3",
			map[string]lookup{"n": {directed, false}, "p": {practice, false}}},
		{"ListCheckoutsWithoutCheckIn", "listCheckoutsWithoutCheckIn", "$1, $2, $3, 51",
			map[string]lookup{"pv": {directed, false}, "i": {directed, true}}},
	}
	alias := regexp.MustCompile(`\btransactions ([a-z]+)\b`)
	scan := regexp.MustCompile(`^\s*(?:->\s+)?((?:Parallel )?(?:Index Only Scan|Index Scan Backward|Index Scan|Bitmap Heap Scan|Seq Scan))(?: using (\S+))? on ` + chainCopy + ` ([a-z]+)(?:_\d+)?\b`)
	for _, tc := range cases {
		stmt := shippedStoreSQL(t, "anomalies.sql.go", tc.constName)
		if !strings.Contains(stmt, "FROM transactions ") {
			t.Fatalf("PREMISE: %s reads no `transactions <alias>`", tc.name)
		}
		stmt = alias.ReplaceAllString(stmt, chainCopy+" $1")
		for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				name := "t157_" + strings.ToLower(tc.name)
				for _, s := range []string{
					`SET LOCAL plan_cache_mode = ` + mode,
					`PREPARE ` + name + ` AS ` + stmt,
				} {
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
					if m[2] != want.index {
						t.Errorf("%s reads %s with %s %s, want %s:\n%s", tc.name, m[3], m[1], m[2], want.index, strings.Join(plan, "\n"))
					}
					if want.mayFilter {
						continue
					}
					for _, next := range plan[i+1:] {
						if strings.Contains(next, "->") {
							break
						}
						if strings.Contains(next, "Filter:") {
							t.Errorf("%s filters rows of %s on %s: %q", tc.name, m[3], m[2], strings.TrimSpace(next))
						}
					}
				}
				for a := range tc.lookups {
					if seen[a] == 0 {
						t.Errorf("PREMISE: %s's plan has no scan of %s:\n%s", tc.name, a, strings.Join(plan, "\n"))
					}
				}
			})
		}
	}
}
