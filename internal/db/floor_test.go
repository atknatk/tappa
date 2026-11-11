package db

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// floor_test.go -- T272: CountOnTheFloorByVenue, The Pass's "on the floor now", reshaped the way
// T245 reshaped GetLastOpenTransaction, so a panel load no longer reads every `out` the business
// ever recorded.
//
// The reshape claims the SAME ANSWER by logic alone (db/queries/transactions.sql, the T272 block):
// "each person's latest `in` in the window with no later `out`" is "each person's latest `in` in the
// window, if no `out` follows it", and "no `out` follows it" is "no non-practice one and no practice
// one". So the first test runs the statement as it was before T272 (floorBefore, verbatim) and the
// shipped one over the shapes the floor meets and requires the same rows at every window start that
// matters. Every row of a shape is written at a venue of its own, so the venues the statement
// returns name exactly the entries it picked -- agreement on the counts is agreement on the rows.
// The second pins what the reshape is FOR: on a long history, the two halves of "a later `out`" are
// probes of 00091's and 00092's indexes, and the whole statement reads a bounded number of pages.
//
// Both run on lastopen_test.go's temporary copy of transactions (every index, transactions' policy,
// no CHECK -- so a practice `out` and a row with no person can be written), in a transaction that
// is always rolled back.

// floorBefore is CountOnTheFloorByVenue before T272, verbatim (the text sqlc generated).
const floorBefore = `SELECT t.location_id, t.site_location_id, l.name AS location_name, count(*) AS people
FROM transactions t
LEFT JOIN locations l ON l.tenant_id = t.tenant_id AND l.id = t.location_id
WHERE t.tenant_id = $1
  AND t.occurred_at >= $2
  AND t.id IN (
      SELECT DISTINCT ON (x.employee_id) x.id
      FROM transactions x
      WHERE x.tenant_id = $1
        AND x.employee_id IS NOT NULL
        AND x.type = 'in'
        AND NOT x.practice
        AND x.occurred_at >= $2
        AND NOT EXISTS (
            SELECT 1
            FROM transactions o
            WHERE o.tenant_id = $1
              AND o.employee_id = x.employee_id
              AND o.type = 'out'
              AND o.occurred_at > x.occurred_at
        )
      ORDER BY x.employee_id, x.occurred_at DESC, x.id DESC
  )
GROUP BY t.location_id, t.site_location_id, l.name
ORDER BY count(*) DESC, l.name, t.location_id, t.site_location_id
`

// floorStatements returns the shipped statement and the pre-T272 one, both pointed at the copy.
func floorStatements(t *testing.T) (shipped, before string) {
	t.Helper()
	shipped = shippedStoreSQL(t, "transactions.sql.go", "countOnTheFloorByVenue")
	alias := regexp.MustCompile(`\btransactions ([a-z]+)\b`)
	shipped = alias.ReplaceAllString(shipped, chainCopy+" $1")
	before = alias.ReplaceAllString(floorBefore, chainCopy+" $1")
	if strings.TrimSpace(shipped[strings.Index(shipped, "\n")+1:]) == strings.TrimSpace(before) {
		t.Fatal("PREMISE: the shipped statement is the pre-T272 one")
	}
	return shipped, before
}

// flRow is one row of a floor shape: its id's last digits (the order of a same-instant tie),
// minutes after the shape's base, direction ("" = none), and how it differs from an ordinary ok
// nfc tap of the shape's own person in the shape's own business.
type flRow struct {
	id       int
	at       int // minutes after the shape's base
	typ      string
	practice bool
	channel  string // "" = nfc
	verdict  string // "" = ok
	other    string // "" | "tenant" (another business, same person id) | "person" (same business) | "nobody" (no employee)
	site     bool   // taken at a holding peer's venue: site_location_id set
}

const floorOtherTenant = "00000000-0000-4000-8000-00000000e272"

// TestCountOnTheFloorByVenue_AgreesWithTheAntiJoinItReplaced is T272's "the same answer": over
// every shape, the shipped statement and floorBefore return the same rows at every window start a
// row of the shape sits at, a microsecond either side of it, and before and after all of them; and
// with the window open over every row the venues are the entries the shape names (so agreement is
// not two empty answers).
func TestCountOnTheFloorByVenue_AgreesWithTheAntiJoinItReplaced(t *testing.T) {
	ctx, tx := lastOpenTx(t)
	lastOpenCopy(t, ctx, tx)
	shipped, before := floorStatements(t)

	flood := func(from, n int) []flRow {
		var rows []flRow
		for g := 0; g < n; g++ {
			v := "reject"
			if g%2 == 0 {
				v = "ignored"
			}
			rows = append(rows, flRow{id: 500 + g, at: from + g, verdict: v})
		}
		return rows
	}
	shapes := []struct {
		name string
		rows []flRow
		want []int // the ids on the floor with the window open over every row
	}{
		{"nothing", nil, nil},
		{"one open in", []flRow{{id: 1, at: 0, typ: "in"}}, []int{1}},
		{"in then out", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 60, typ: "out"}}, nil},
		{"in, then refused and ignored taps", append([]flRow{{id: 1, at: 0, typ: "in"}}, flood(1, 40)...), []int{1}},
		{"in, refused and ignored taps, then out", append(append([]flRow{{id: 1, at: 0, typ: "in"}}, flood(1, 40)...), flRow{id: 2, at: 100, typ: "out"}), nil},
		{"a flagged in counts", []flRow{{id: 1, at: 0, typ: "in", verdict: "flag"}}, []int{1}},
		{"a flagged out closes", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", verdict: "flag"}}, nil},
		{"practice in after the open in", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "in", practice: true}}, []int{1}},
		{"practice in before the open in", []flRow{{id: 1, at: -30, typ: "in", practice: true}, {id: 2, at: 0, typ: "in"}}, []int{2}},
		{"only a practice in", []flRow{{id: 1, at: 0, typ: "in", practice: true}}, nil},
		{"practice in after a closed day", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in", practice: true}}, nil},
		{"a practice out after the in closes it", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", practice: true}}, nil},
		{"a practice out before the in does not", []flRow{{id: 1, at: -30, typ: "out", practice: true}, {id: 2, at: 0, typ: "in"}}, []int{2}},
		{"a practice out at the in's instant does not", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 0, typ: "out", practice: true}}, []int{1}},
		{"a manual out closes", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", channel: "manual"}}, nil},
		{"a manual in after an out counts", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in", channel: "manual"}}, []int{3}},
		{"a qr in counts", []flRow{{id: 1, at: 0, typ: "in", channel: "qr"}}, []int{1}},
		{"an in at a peer's venue counts there", []flRow{{id: 1, at: 0, typ: "in", site: true}}, []int{1}},
		{"another business's out does not close", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", other: "tenant"}}, []int{1}},
		{"another business's in is not on this floor", []flRow{{id: 1, at: 0, typ: "in", other: "tenant"}}, nil},
		{"another business's practice out does not close", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", practice: true, other: "tenant"}}, []int{1}},
		{"another business's later in is not the entry", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "in", other: "tenant"}}, []int{1}},
		{"another person's out does not close", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", other: "person"}}, []int{1}},
		{"two people, each counted", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 10, typ: "in", other: "person"}}, []int{1, 2}},
		{"an in with no person is nobody", []flRow{{id: 1, at: 0, typ: "in", other: "nobody"}}, nil},
		{"an out with no person closes nothing", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out", other: "nobody"}}, []int{1}},
		{"two ins and no out count once", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "in"}}, []int{2}},
		{"in, out, in", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in"}}, []int{3}},
		{"in, out, in, out", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 60, typ: "in"}, {id: 4, at: 90, typ: "out"}}, nil},
		{"in, out and in at one instant", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 30, typ: "out"}, {id: 3, at: 30, typ: "in"}}, []int{3}},
		{"two outs after two ins", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 10, typ: "in"}, {id: 3, at: 20, typ: "out"}, {id: 4, at: 30, typ: "out"}}, nil},
		{"same instant, the out's id later", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 0, typ: "out"}}, []int{1}},
		{"same instant, the out's id earlier", []flRow{{id: 1, at: 0, typ: "out"}, {id: 2, at: 0, typ: "in"}}, []int{2}},
		{"two same-instant ins", []flRow{{id: 1, at: 0, typ: "in"}, {id: 2, at: 0, typ: "in"}}, []int{2}},
		{"two same-instant ins, the larger id written first", []flRow{{id: 2, at: 0, typ: "in"}, {id: 1, at: 0, typ: "in"}}, []int{2}},
		{"two same-instant ins and an out at that instant", []flRow{{id: 1, at: 0, typ: "in"}, {id: 3, at: 0, typ: "out"}, {id: 2, at: 0, typ: "in"}}, []int{2}},
		{"an out before every in", []flRow{{id: 1, at: -10, typ: "out"}, {id: 2, at: 0, typ: "in"}}, []int{2}},
		// The night shift: 22:00 UTC in, 02:00 UTC out the next day -- by instant, not by calendar day.
		{"night shift closed after midnight", []flRow{{id: 1, at: -600, typ: "in"}, {id: 2, at: -360, typ: "out"}}, nil},
		{"night shift still open after midnight", []flRow{{id: 1, at: -600, typ: "in"}}, []int{1}},
		{"yesterday's open in, today's in", []flRow{{id: 1, at: -1440, typ: "in"}, {id: 2, at: 0, typ: "in"}}, []int{2}},
	}

	// Every shape is its own business (the statement is business-wide), written first, then asked
	// in three passes: the planner free, index scans off (the DISTINCT ON's (occurred_at, id)
	// tie-break then has only the sort to go by; the same-instant shapes write their rows in both
	// physical orders), and the planner free in a session at UTC+14 (the answer is by instant).
	base := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	type written struct {
		tenant   string
		starts   []time.Time
		wantRows string
	}
	var all []written
	for n, sh := range shapes {
		tenant := fmt.Sprintf("00000000-0000-4000-8272-%012d", n+1)
		person := fmt.Sprintf("00000000-0000-4000-8273-%012d", n+1)
		otherPerson := fmt.Sprintf("00000000-0000-4000-8274-%012d", n+1)
		id := func(r int) string { return fmt.Sprintf("00000000-0000-4000-%04d-%012d", n+1, r) }
		// A venue of the row's own: the statement's venues name the entries it picked.
		venue := func(r int) string { return fmt.Sprintf("00000000-0000-4000-%04d-%012d", 9000+n+1, r) }
		starts := []time.Time{base.Add(-48 * time.Hour), base.Add(48 * time.Hour)}
		for _, r := range sh.rows {
			rowTenant, emp := tenant, &person
			switch r.other {
			case "tenant":
				rowTenant = floorOtherTenant
			case "person":
				emp = &otherPerson
			case "nobody":
				emp = nil
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
			loc, site, siteTenant := venue(r.id), (*string)(nil), (*string)(nil)
			if r.site {
				s, st := venue(r.id), floorOtherTenant
				loc, site, siteTenant = venue(1000+r.id), &s, &st
			}
			at := base.Add(time.Duration(r.at) * time.Minute)
			if _, err := tx.Exec(ctx, `INSERT INTO `+chainCopy+` (id, tenant_id, employee_id, type, occurred_at, verdict, channel, practice, location_id, site_location_id, site_tenant_id)
			                           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				id(r.id), rowTenant, emp, typ, at, verdict, channel, r.practice, loc, site, siteTenant); err != nil {
				t.Fatalf("%s: insert row %d: %v", sh.name, r.id, err)
			}
			starts = append(starts, at.Add(-time.Microsecond), at, at.Add(time.Microsecond))
		}
		var want []string
		for _, r := range sh.want {
			var row flRow
			for _, c := range sh.rows {
				if c.id == r {
					row = c
				}
			}
			if row.site {
				want = append(want, venue(1000+r)+"|"+venue(r)+"|<nil>|1")
			} else {
				want = append(want, venue(r)+"|<nil>|<nil>|1")
			}
		}
		sort.Strings(want)
		all = append(all, written{tenant, starts, strings.Join(want, ";")})
	}

	compared := 0
	for _, mode := range []struct {
		name string
		gucs []string
	}{
		{"planner free", []string{`SET LOCAL enable_indexscan = on`, `SET LOCAL enable_indexonlyscan = on`, `SET LOCAL enable_bitmapscan = on`, `SET LOCAL TimeZone = 'UTC'`}},
		{"index scans off", []string{`SET LOCAL enable_indexscan = off`, `SET LOCAL enable_indexonlyscan = off`, `SET LOCAL enable_bitmapscan = off`}},
		{"planner free at UTC+14", []string{`SET LOCAL enable_indexscan = on`, `SET LOCAL enable_indexonlyscan = on`, `SET LOCAL enable_bitmapscan = on`, `SET LOCAL TimeZone = 'Pacific/Kiritimati'`}},
	} {
		for _, g := range mode.gucs {
			if _, err := tx.Exec(ctx, g); err != nil {
				t.Fatalf("%s: %v", g, err)
			}
		}
		if mode.name == "index scans off" {
			// PREMISE: the second pass really sorts -- no index is read anywhere in the statement.
			rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF) `+shipped+` /* `+mode.name+` */`, all[1].tenant, base)
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
			ask := func(stmt string, since time.Time) string {
				t.Helper()
				// The mode is in the text so pgx prepares, and the planner plans, each mode's statement
				// under that mode's settings (T245: a statement prepared in the first pass keeps its
				// plan in the second).
				rows, err := tx.Query(ctx, stmt+` /* `+mode.name+` */`, w.tenant, since)
				if err != nil {
					t.Fatalf("%s (%s) since %s: %v", sh.name, mode.name, since.Format(time.RFC3339Nano), err)
				}
				var got []string
				for rows.Next() {
					var (
						loc, site *string
						name      *string
						people    int64
					)
					if err := rows.Scan(&loc, &site, &name, &people); err != nil {
						t.Fatal(err)
					}
					str := func(p *string) string {
						if p == nil {
							return "<nil>"
						}
						return *p
					}
					got = append(got, str(loc)+"|"+str(site)+"|"+str(name)+"|"+strconv.FormatInt(people, 10))
				}
				if err := rows.Err(); err != nil {
					t.Fatalf("%s (%s): %v", sh.name, mode.name, err)
				}
				sort.Strings(got)
				return strings.Join(got, ";")
			}
			for _, since := range w.starts {
				now, then := ask(shipped, since), ask(before, since)
				compared++
				if now != then {
					t.Errorf("%s (%s) since %s: the shipped statement answers %q, the one it replaced %q", sh.name, mode.name, since.Format(time.RFC3339Nano), now, then)
				}
			}
			if got := ask(shipped, w.starts[0]); got != w.wantRows {
				t.Errorf("%s (%s): with the window open over every row the floor is %q, want %q", sh.name, mode.name, got, w.wantRows)
			}
		}
	}
	if compared < 9*len(shapes) {
		t.Fatalf("PREMISE: %d comparisons over %d shapes in three modes", compared, len(shapes))
	}
}

// TestCountOnTheFloorByVenue_ProbesEachEntryInsteadOfTheOutHistory is T272's plan pin. On one
// business whose 20 people have two years of 09:00 in / 17:00 out each, 12 of them in since six
// hours and 8 in and out today, the shipped statement -- generic and custom plan, as the owner and
// as tappa_app in the business's context -- asks "a later non-practice `out`" (o) as one index probe
// per entry and "a later practice `out`" (p) of 00092's index, never reads the copy sequentially,
// counts the 12, and reads at most floorBufferCeiling pages, executed. Before T272 the same data cost 516-544
// pages: a Seq Scan of every `out` the business recorded, hashed (the T272 block in
// db/queries/transactions.sql has the numbers).
func TestCountOnTheFloorByVenue_ProbesEachEntryInsteadOfTheOutHistory(t *testing.T) {
	const floorBufferCeiling = 250
	ctx, tx := lastOpenTx(t)
	lastOpenCopy(t, ctx, tx)
	shipped, _ := floorStatements(t)

	tenant := "00000000-0000-4000-8000-00000000f272"
	for _, s := range []string{
		`CREATE TEMPORARY TABLE t272_people ON COMMIT DROP AS
		   SELECT ('00000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid AS id, n,
		          ('00000000-0000-4000-9000-' || lpad((n % 9)::text, 12, '0'))::uuid AS venue
		     FROM generate_series(1, 20) AS n`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel)
		 SELECT '` + tenant + `', p.id, p.venue, x.type, date_trunc('day', now()) - make_interval(days => d) + x.at, 'ok', 'nfc'
		   FROM t272_people AS p, generate_series(2, 730) AS d,
		        (VALUES ('in', interval '9 hours'), ('out', interval '17 hours')) AS x(type, at)`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel)
		 SELECT '` + tenant + `', p.id, p.venue, 'in', now() - interval '6 hours', 'ok', 'nfc' FROM t272_people AS p WHERE p.n <= 12`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel)
		 SELECT '` + tenant + `', p.id, p.venue, x.type, now() - x.ago, 'ok', 'nfc'
		   FROM t272_people AS p, (VALUES ('in', interval '10 hours'), ('out', interval '2 hours')) AS x(type, ago) WHERE p.n > 12`,
		`INSERT INTO ` + chainCopy + ` (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, practice)
		 SELECT '` + tenant + `', p.id, p.venue, 'in', now() - interval '800 days', 'ok', 'nfc', true FROM t272_people AS p`,
		`ANALYZE ` + chainCopy,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	// o may be either index that leads with (tenant, person, occurred_at): 00091's (whose predicate
	// the probe proves) or 00005's -- with no refused or ignored taps after the entries they are the
	// same size here and the planner's pick between them is a tie (measured: 00005's in this test,
	// 00091's on the scratch shapes with a flood). What the pin holds o to is being a PROBE PER ENTRY:
	// its index condition carries the entry's person and instant. p reads 00092's, the only index
	// whose predicate a practice `out` proves.
	lookups := map[string][]string{
		"o": {chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_directed_idx"),
			chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_occurred_idx")},
		"p": {chainIndexCopy(t, ctx, tx, "transactions_tenant_employee_practice_idx")},
	}
	scan := regexp.MustCompile(`^\s*(?:->\s+)?((?:Parallel )?(?:Index Only Scan Backward|Index Only Scan|Index Scan Backward|Index Scan|Bitmap Heap Scan|Seq Scan))(?: using (\S+))? on ` + chainCopy + ` ([a-z]+)(?:_\d+)?\b`)
	bitmapIndex := regexp.MustCompile(`Bitmap Index Scan on (\S+)`)
	buffers := regexp.MustCompile(`\b(?:hit|read)=(\d+)`)

	for _, run := range []struct{ role, mode string }{
		{"owner", "force_generic_plan"}, {"owner", "force_custom_plan"},
		{"tappa_app", "force_generic_plan"}, {"tappa_app", "force_custom_plan"},
	} {
		t.Run(run.role+"/"+run.mode, func(t *testing.T) {
			setup := []string{`RESET ROLE`, `SET LOCAL plan_cache_mode = ` + run.mode}
			if run.role == "tappa_app" {
				setup = append(setup, `SET LOCAL ROLE tappa_app`,
					`SELECT set_config('app.tenant_id', '`+tenant+`', true)`)
			}
			t.Cleanup(func() {
				for _, s := range []string{`DEALLOCATE t272_floor`, `RESET ROLE`} {
					if _, err := tx.Exec(ctx, s); err != nil {
						t.Errorf("%s: %v", s, err)
					}
				}
			})
			for _, s := range append(setup, `PREPARE t272_floor AS `+shipped) {
				if _, err := tx.Exec(ctx, s); err != nil {
					t.Fatalf("%s: %v", s, err)
				}
			}
			args := `('` + tenant + `', now() - interval '18 hours')`
			rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, SUMMARY OFF) EXECUTE t272_floor`+args)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(plan, "\n")

			// The answer: 12 people on the floor.
			rs, err := tx.Query(ctx, `EXECUTE t272_floor`+args)
			if err != nil {
				t.Fatal(err)
			}
			var people int64
			for rs.Next() {
				var (
					loc, site, name *string
					n               int64
				)
				if err := rs.Scan(&loc, &site, &name, &n); err != nil {
					t.Fatal(err)
				}
				people += n
			}
			if err := rs.Err(); err != nil {
				t.Fatal(err)
			}
			if people != 12 {
				t.Errorf("the floor counts %d people, want 12:\n%s", people, text)
			}

			seen := map[string]bool{}
			for i, line := range plan {
				m := scan.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				if m[1] == "Seq Scan" || m[1] == "Parallel Seq Scan" {
					t.Errorf("the plan reads the copy sequentially (alias %s) -- a history-wide read:\n%s", m[3], text)
				}
				want, ok := lookups[m[3]]
				if !ok {
					continue // t and x: the entry's own row and the window
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
				if !slices.Contains(want, index) {
					t.Errorf("%s is read with %s %s, want one of %v:\n%s", m[3], m[1], index, want, text)
				}
				if m[3] == "o" {
					cond := ""
					for _, d := range plan[i+1:] {
						if strings.Contains(d, "->") {
							break
						}
						if c, ok := strings.CutPrefix(strings.TrimSpace(d), "Index Cond: "); ok {
							cond = c
						}
					}
					if !strings.Contains(cond, "employee_id = ") || !strings.Contains(cond, "occurred_at > ") {
						t.Errorf("o is not a probe per entry (index condition %q):\n%s", cond, text)
					}
				}
				if run.role == "tappa_app" && !chainGuarded(plan, i) {
					t.Errorf("PREMISE: tappa_app's scan of %s carries no row-security predicate:\n%s", m[3], text)
				}
			}
			for a := range lookups {
				if !seen[a] {
					t.Errorf("the plan has no scan of %s -- the statement is not T272's shape:\n%s", a, text)
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
			t.Logf("%s/%s: %d pages", run.role, run.mode, total)
			if total == 0 || total > floorBufferCeiling {
				t.Errorf("%s/%s read %d pages, want 1..%d:\n%s", run.role, run.mode, total, floorBufferCeiling, text)
			}
		})
	}
}
