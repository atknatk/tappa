package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// lastopen_test.go -- T245: GetLastOpenTransaction, the direction toggle's input (CLAUDE.md §5),
// reshaped so a tap no longer walks the person's refused and ignored taps after their open entry.
//
// The reshape claims the SAME ANSWER by logic alone (db/queries/transactions.sql, the T245 block):
// "the latest `in` with no later `out`" is "the latest `in`, if no `out` follows it", and "no `out`
// follows it" is "no non-practice one and no practice one". So the first test runs the statement as
// it was before T245 (lastOpenBefore, verbatim) and the shipped one over the shapes the toggle meets
// -- practice rows on either side of the entry, an `out` after it, a manager's rows, another
// business's and another person's rows, a flood of directionless rows, same-instant ties, the @at
// bound on both sides -- and requires the same row (or none) at every instant that matters. The
// second pins what the reshape is FOR: on a flood, each of its three reads is its partial index's and
// the whole statement reads a handful of pages.
//
// Both run on a temporary copy of transactions (chainindex_test.go's arrangement: LIKE ... INCLUDING
// INDEXES, so the copy carries every index the real table has; the policy copied from the catalogue
// for the tappa_app runs), inside a transaction that is always rolled back. The copy carries no CHECK
// (LIKE copies none), which is what lets the first test write a practice `out` -- a row no writer
// has ever produced, but one no constraint forbids either, so the reshape must agree on it too.

// lastOpenBefore is GetLastOpenTransaction's FROM ... LIMIT before T245, verbatim (the column list is
// unchanged and is taken from the shipped text).
const lastOpenBefore = `FROM transactions t
WHERE t.tenant_id = $1
  AND t.employee_id = $2
  AND t.type = 'in'
  AND NOT t.practice
  AND t.occurred_at <= $3
  AND NOT EXISTS (
      SELECT 1
      FROM transactions o
      WHERE o.tenant_id = t.tenant_id
        AND o.employee_id = t.employee_id
        AND o.type = 'out'
        AND (o.occurred_at, o.id) > (t.occurred_at, t.id)
        AND o.occurred_at <= $3
  )
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT 1`

// lastOpenStatements returns the shipped statement and the pre-T245 one, both pointed at the copy.
func lastOpenStatements(t *testing.T) (shipped, before string) {
	t.Helper()
	shipped = shippedStoreSQL(t, "transactions.sql.go", "getLastOpenTransaction")
	i := strings.Index(shipped, "FROM transactions t\n")
	if i < 0 {
		t.Fatalf("PREMISE: the shipped GetLastOpenTransaction has no `FROM transactions t`:\n%s", shipped)
	}
	before = shipped[:i] + lastOpenBefore
	alias := regexp.MustCompile(`\btransactions ([a-z]+)\b`)
	shipped = alias.ReplaceAllString(shipped, chainCopy+" $1")
	before = alias.ReplaceAllString(before, chainCopy+" $1")
	if shipped == before {
		t.Fatal("PREMISE: the shipped statement is the pre-T245 one")
	}
	return shipped, before
}

// lastOpenCopy creates the copy of transactions in tx (owner), with transactions' policy and
// tappa_app's SELECT on it.
func lastOpenCopy(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(ctx, `CREATE TEMPORARY TABLE `+chainCopy+` (LIKE public.transactions INCLUDING DEFAULTS INCLUDING INDEXES) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	var using, check string
	if err := tx.QueryRow(ctx, `SELECT pg_get_expr(polqual, polrelid), pg_get_expr(polwithcheck, polrelid)
	                              FROM pg_policy WHERE polrelid = 'public.transactions'::regclass
	                               AND polname = 'transactions_tenant_isolation'`).Scan(&using, &check); err != nil {
		t.Fatalf("read transactions' policy: %v", err)
	}
	for _, s := range []string{
		`ALTER TABLE ` + chainCopy + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + chainCopy + ` FORCE ROW LEVEL SECURITY`,
		`CREATE POLICY t245_tenant_isolation ON ` + chainCopy + ` USING (` + using + `) WITH CHECK (` + check + `)`,
		`GRANT SELECT ON ` + chainCopy + ` TO tappa_app`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// lastOpenTx begins a transaction on the owner pool that is rolled back when the test ends.
func lastOpenTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	owner := ownerDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	tx, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rb := tx.Rollback(context.Background()); rb != nil && !errors.Is(rb, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", rb)
		}
	})
	return ctx, tx
}

// loRow is one row of a shape: its id's last digits (the order of a same-instant tie), minutes after
// the shape's base, direction ("" = none), and how it differs from an ordinary ok nfc tap of the
// shape's own person in the shape's own business.
type loRow struct {
	id       int
	at       int // minutes after the shape's base
	typ      string
	practice bool
	channel  string // "" = nfc
	verdict  string // "" = ok
	other    string // "" | "tenant" (another business, same person id) | "person" (same business)
}

const (
	loTenant      = "00000000-0000-4000-8000-00000000f245"
	loOtherTenant = "00000000-0000-4000-8000-00000000e245"
)

// TestGetLastOpenTransaction_AgreesWithTheAntiJoinItReplaced is T245's "the same row": over every
// shape, the shipped statement and lastOpenBefore return the same row -- or both none -- at every
// instant a row of the shape sits at, a microsecond either side of it, and after all of them; and at
// that last instant the answer is the one the shape names (so agreement is not two empty answers).
func TestGetLastOpenTransaction_AgreesWithTheAntiJoinItReplaced(t *testing.T) {
	ctx, tx := lastOpenTx(t)
	lastOpenCopy(t, ctx, tx)
	shipped, before := lastOpenStatements(t)

	flood := func(from, n int) []loRow {
		var rows []loRow
		for g := 0; g < n; g++ {
			v := "reject"
			if g%2 == 0 {
				v = "ignored"
			}
			rows = append(rows, loRow{id: 500 + g, at: from + g, verdict: v})
		}
		return rows
	}
	shapes := []struct {
		name string
		rows []loRow
		want int // the id open after every row; 0 = none
	}{
		{"nothing", nil, 0},
		{"one open in", []loRow{{id: 1, at: 0, typ: "in"}}, 1},
		{"in then out", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 60, typ: "out"}}, 0},
		{"in, then refused and ignored taps", append([]loRow{{id: 1, at: 0, typ: "in"}}, flood(1, 40)...), 1},
		{"in, refused and ignored taps, then out", append(append([]loRow{{id: 1, at: 0, typ: "in"}}, flood(1, 40)...), loRow{id: 2, at: 100, typ: "out"}), 0},
		{"a flagged in", []loRow{{id: 1, at: 0, typ: "in", verdict: "flag"}}, 1},
		{"a flagged out closes", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", verdict: "flag"}}, 0},
		{"practice in after the open in", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "in", practice: true}}, 1},
		{"practice in before the open in", []loRow{{id: 1, at: -30, typ: "in", practice: true}, {id: 2, at: 0, typ: "in"}}, 2},
		{"only a practice in", []loRow{{id: 1, at: 0, typ: "in", practice: true}}, 0},
		{"practice in after a closed day", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in", practice: true}}, 0},
		{"a practice out after the in closes it", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", practice: true}}, 0},
		{"a practice out before the in does not", []loRow{{id: 1, at: -30, typ: "out", practice: true}, {id: 2, at: 0, typ: "in"}}, 2},
		{"a manual out closes", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", channel: "manual"}}, 0},
		{"a manual in after an out", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in", channel: "manual"}}, 3},
		{"a qr in", []loRow{{id: 1, at: 0, typ: "in", channel: "qr"}}, 1},
		{"another business's out does not close", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", other: "tenant"}}, 1},
		{"another business's in is not open", []loRow{{id: 1, at: 0, typ: "in", other: "tenant"}}, 0},
		{"another business's practice out does not close", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", practice: true, other: "tenant"}}, 1},
		{"another business's later in is not the entry", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "in", other: "tenant"}}, 1},
		{"another person's out does not close", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", other: "person"}}, 1},
		{"two ins and no out", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "in"}}, 2},
		{"in, out, in", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in"}}, 3},
		{"in, out, in, out", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in"}, {id: 4, at: 90, typ: "out"}}, 0},
		{"two outs after two ins", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 10, typ: "in"}, {id: 3, at: 20, typ: "out"}, {id: 4, at: 30, typ: "out"}}, 0},
		{"same instant, the out's id later", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 0, typ: "out"}}, 0},
		{"same instant, the out's id earlier", []loRow{{id: 1, at: 0, typ: "out"}, {id: 2, at: 0, typ: "in"}}, 2},
		{"two same-instant ins", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 0, typ: "in"}}, 2},
		{"two same-instant ins, the larger id written first", []loRow{{id: 2, at: 0, typ: "in"}, {id: 1, at: 0, typ: "in"}}, 2},
		{"same instant practice out, id later", []loRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 0, typ: "out", practice: true}}, 0},
		{"an out before every in", []loRow{{id: 1, at: -10, typ: "out"}, {id: 2, at: 0, typ: "in"}}, 2},
	}

	// Every shape is written first, then asked twice: with the planner free, and with index scans
	// off. The second pass is for the (occurred_at, id) tie-break of the ORDER BY: with an index
	// the backward walk of 00094's key (occurred_at, id) already yields the larger id first, so a
	// deleted `l.id DESC` stays green there (third-eye round, T245); a sort over a sequential scan
	// has only the ORDER BY to go by. The two same-instant shapes write their rows in both
	// physical orders, so a sort that keeps either order is caught.
	base := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	type written struct {
		person   string
		instants []time.Time
		want     string
	}
	var all []written
	for n, sh := range shapes {
		person := fmt.Sprintf("00000000-0000-4000-8245-%012d", n+1)
		otherPerson := fmt.Sprintf("00000000-0000-4000-8246-%012d", n+1)
		id := func(r int) string { return fmt.Sprintf("00000000-0000-4000-%04d-%012d", n+1, r) }
		var instants []time.Time
		for _, r := range sh.rows {
			tenant, emp := loTenant, person
			switch r.other {
			case "tenant":
				tenant = loOtherTenant
			case "person":
				emp = otherPerson
			}
			channel, verdict := r.channel, r.verdict
			if channel == "" {
				channel = "nfc"
			}
			if verdict == "" {
				verdict = "ok"
			}
			var typ *string
			if r.typ != "" {
				typ = &r.typ
			}
			at := base.Add(time.Duration(r.at) * time.Minute)
			if _, err := tx.Exec(ctx, `INSERT INTO `+chainCopy+` (id, tenant_id, employee_id, type, occurred_at, verdict, channel, practice)
			                           VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				id(r.id), tenant, emp, typ, at, verdict, channel, r.practice); err != nil {
				t.Fatalf("%s: insert row %d: %v", sh.name, r.id, err)
			}
			instants = append(instants, at.Add(-time.Microsecond), at, at.Add(time.Microsecond))
		}
		instants = append(instants, base.Add(-24*time.Hour), base.Add(24*time.Hour))
		want := "none"
		if sh.want != 0 {
			want = id(sh.want)
		}
		all = append(all, written{person, instants, want})
	}

	compared := 0
	for _, mode := range []struct {
		name string
		gucs []string
	}{
		{"planner free", []string{`SET LOCAL enable_indexscan = on`, `SET LOCAL enable_indexonlyscan = on`, `SET LOCAL enable_bitmapscan = on`}},
		{"index scans off", []string{`SET LOCAL enable_indexscan = off`, `SET LOCAL enable_indexonlyscan = off`, `SET LOCAL enable_bitmapscan = off`}},
	} {
		for _, g := range mode.gucs {
			if _, err := tx.Exec(ctx, g); err != nil {
				t.Fatalf("%s: %v", g, err)
			}
		}
		if mode.name == "index scans off" {
			// PREMISE: the second pass really sorts -- no index is read for any of the statement's
			// four scans.
			rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF) `+shipped+` /* `+mode.name+` */`, loTenant, all[0].person, base)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				t.Fatal(err)
			}
			if text := strings.Join(plan, "\n"); strings.Contains(text, "Index") || !strings.Contains(text, "Sort") {
				t.Fatalf("PREMISE: with index scans off the statement still reads an index or does not sort:\n%s", text)
			}
		}
		for n, sh := range shapes {
			w := all[n]
			ask := func(stmt string, at time.Time) string {
				t.Helper()
				var got string
				// The mode is in the text so pgx prepares, and the planner plans, each mode's statement
				// under that mode's settings: a statement prepared in the first pass would keep its
				// index plan in the second (measured: the deleted tie-break stayed green that way).
				err := tx.QueryRow(ctx, `SELECT id::text FROM (`+stmt+`) AS r /* `+mode.name+` */`, loTenant, w.person, at).Scan(&got)
				if errors.Is(err, pgx.ErrNoRows) {
					return "none"
				}
				if err != nil {
					t.Fatalf("%s (%s) at %s: %v", sh.name, mode.name, at.Format(time.RFC3339Nano), err)
				}
				return got
			}
			for _, at := range w.instants {
				now, then := ask(shipped, at), ask(before, at)
				compared++
				if now != then {
					t.Errorf("%s (%s) at %s: the shipped statement answers %s, the one it replaced %s", sh.name, mode.name, at.Format(time.RFC3339Nano), now, then)
				}
			}
			if got := ask(shipped, w.instants[len(w.instants)-1]); got != w.want {
				t.Errorf("%s (%s): after every row the open entry is %s, want %s", sh.name, mode.name, got, w.want)
			}
		}
	}
	if compared < 6*len(shapes) {
		t.Fatalf("PREMISE: %d comparisons over %d shapes in two modes", compared, len(shapes))
	}
}

// TestGetLastOpenTransaction_ReadsTheEntryAndOneProbePerHalf is T245's plan pin. On one business
// whose people have a year of 09:00 in / 17:00 out each, one of them checked in with 20 000
// refused and ignored taps after the entry and another checked OUT with the same flood before the
// checkout (the two shapes the old anti-join walked), the shipped statement -- generic and custom
// plan, as the owner and as tappa_app in the business's context -- reads the latest entry (l) from
// 00094's index, the non-practice half (o) from 00091's and the practice half (p) from 00092's, and
// the whole statement reads at most loBufferCeiling pages, executed. Before T245 the same data cost
// hundreds to thousands of pages (the T245 block in db/queries/transactions.sql has the numbers).
func TestGetLastOpenTransaction_ReadsTheEntryAndOneProbePerHalf(t *testing.T) {
	const loBufferCeiling = 40
	ctx, tx := lastOpenTx(t)
	lastOpenCopy(t, ctx, tx)
	shipped, _ := lastOpenStatements(t)

	checkedIn := "00000000-0000-4000-8000-000000000001"
	checkedOut := "00000000-0000-4000-8000-000000000002"
	for _, s := range []string{
		`CREATE TEMPORARY TABLE t245_people ON COMMIT DROP AS
		   SELECT ('00000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid AS id, n FROM generate_series(1, 20) AS n`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel)
		 SELECT '` + loTenant + `', p.id, x.type, date_trunc('day', now()) - make_interval(days => d) + x.at, 'ok', 'nfc'
		   FROM t245_people AS p, generate_series(2, 366) AS d,
		        (VALUES ('in', interval '9 hours'), ('out', interval '17 hours')) AS x(type, at)`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel)
		 SELECT '` + loTenant + `', p.id, 'in', now() - interval '6 hours', 'ok', 'nfc' FROM t245_people AS p`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel)
		 SELECT '` + loTenant + `', p.id, NULL, now() - interval '6 hours' + g * interval '0.5 second',
		        CASE WHEN g % 2 = 0 THEN 'ignored' ELSE 'reject' END, 'nfc'
		   FROM t245_people AS p, generate_series(1, 20000) AS g WHERE p.n IN (1, 2)`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel)
		 SELECT '` + loTenant + `', p.id, 'out', now() - interval '1 minute', 'ok', 'nfc' FROM t245_people AS p WHERE p.n = 2`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, type, occurred_at, verdict, channel, practice)
		 SELECT '` + loTenant + `', p.id, 'in', now() - interval '400 days', 'ok', 'nfc', true FROM t245_people AS p`,
		`ANALYZE ` + chainCopy,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	lookups := map[string]string{
		"l": chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_checkin_idx"),
		"o": chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_directed_idx"),
		"p": chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_practice_idx"),
	}
	scan := regexp.MustCompile(`^\s*(?:->\s+)?((?:Parallel )?(?:Index Only Scan Backward|Index Only Scan|Index Scan Backward|Index Scan|Bitmap Heap Scan|Seq Scan))(?: using (\S+))? on ` + chainCopy + ` ([a-z]+)(?:_\d+)?\b`)
	bitmapIndex := regexp.MustCompile(`Bitmap Index Scan on (\S+)`)
	buffers := regexp.MustCompile(`\b(?:hit|read)=(\d+)`)

	for _, person := range []struct{ name, id, want string }{
		{"checked in", checkedIn, "row"}, {"checked out", checkedOut, "none"},
	} {
		for _, run := range []struct{ role, mode string }{
			{"owner", "force_generic_plan"}, {"owner", "force_custom_plan"},
			{"tappa_app", "force_generic_plan"}, {"tappa_app", "force_custom_plan"},
		} {
			t.Run(person.name+"/"+run.role+"/"+run.mode, func(t *testing.T) {
				setup := []string{`RESET ROLE`, `SET LOCAL plan_cache_mode = ` + run.mode}
				if run.role == "tappa_app" {
					setup = append(setup, `SET LOCAL ROLE tappa_app`,
						`SELECT set_config('app.tenant_id', '`+loTenant+`', true)`)
				}
				t.Cleanup(func() {
					for _, s := range []string{`DEALLOCATE t245_last_open`, `RESET ROLE`} {
						if _, err := tx.Exec(ctx, s); err != nil {
							t.Errorf("%s: %v", s, err)
						}
					}
				})
				for _, s := range append(setup, `PREPARE t245_last_open AS `+shipped) {
					if _, err := tx.Exec(ctx, s); err != nil {
						t.Fatalf("%s: %v", s, err)
					}
				}
				rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, SUMMARY OFF) EXECUTE t245_last_open('`+loTenant+`', '`+person.id+`', now())`)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
				if err != nil {
					t.Fatal(err)
				}
				text := strings.Join(plan, "\n")

				// The answer: the root node's actual rows.
				rootRows := regexp.MustCompile(`actual rows=(\d+)`).FindStringSubmatch(plan[0])
				wantRows := map[string]string{"row": "1", "none": "0"}[person.want]
				if rootRows == nil || rootRows[1] != wantRows {
					t.Errorf("the statement returned %v rows for the %s person, want %s:\n%s", rootRows, person.name, wantRows, text)
				}

				seen := map[string]bool{}
				for i, line := range plan {
					m := scan.FindStringSubmatch(line)
					if m == nil {
						continue
					}
					want, ok := lookups[m[3]]
					if !ok {
						continue // t: the entry's own row, by its key
					}
					seen[m[3]] = true
					index := m[2]
					if m[1] == "Bitmap Heap Scan" {
						for _, next := range plan[i+1:] {
							if b := bitmapIndex.FindStringSubmatch(next); b != nil {
								index = b[1]
								break
							}
						}
					}
					if index != want {
						t.Errorf("%s reads %s with %s %s, want %s:\n%s", person.name, m[3], m[1], index, want, text)
					}
					if run.role == "tappa_app" && !chainGuarded(plan, i) {
						t.Errorf("PREMISE: tappa_app's scan of %s carries no row-security predicate:\n%s", m[3], text)
					}
				}
				for a := range lookups {
					if !seen[a] {
						t.Errorf("the plan has no scan of %s -- the statement is not T245's shape:\n%s", a, text)
					}
				}
				// The pages read: the root's Buffers line (the whole statement, executed).
				total := 0
				for _, line := range plan[1:] {
					if strings.Contains(line, "->") {
						break
					}
					if f := strings.TrimSpace(line); strings.HasPrefix(f, "Buffers:") {
						for _, b := range buffers.FindAllStringSubmatch(f, -1) {
							v, err := strconv.Atoi(b[1])
							if err != nil {
								t.Fatalf("buffers %q: %v", f, err)
							}
							total += v
						}
					}
				}
				t.Logf("%s/%s/%s: %d pages", person.name, run.role, run.mode, total)
				if total == 0 || total > loBufferCeiling {
					t.Errorf("%s/%s/%s read %d pages, want 1..%d:\n%s", person.name, run.role, run.mode, total, loBufferCeiling, text)
				}
			})
		}
	}
}
