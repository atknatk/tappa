package tenant

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// employeedirectory_db_test.go -- M11 API-8: the directory's read end (EmployeeDirectory) against
// real Postgres. The HTTP contract is internal/handler/api's TestDirectory_*; here, what needs the
// reader itself: the walk guarantee under concurrent writers (with the overlap scaled to the test's
// write holds) and the own half's RLS.

// directorySQL is the store's constant name, read from the generated file (the store keeps it
// unexported) -- listDirectoryEmployees: $1 tenant_id, $2 after_at, $3 after_id, $4 page_size.
func directorySQL(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "store", "directory.sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	head := "const " + name + " = `"
	i := strings.Index(src, head)
	if i < 0 {
		t.Fatalf("no %s in internal/store/directory.sql.go", name)
	}
	rest := src[i+len(head):]
	return rest[:strings.Index(rest, "`")]
}

// TestEmployeeDirectory_RLSHoldsWithoutTheFilter is the own half's braces measured apart from its
// belt (CLAUDE.md §6: an isolation probe must not carry the filter, or its 0 rows would be the WHERE's):
// the SHIPPED ListDirectoryEmployees with its tenant predicate neutralised, run as tappa_app in KM's
// context and asked for KF's people, answers KM's people alone. CONTROL: the same statement as the
// owner (no RLS) answers both businesses' people.
func TestEmployeeDirectory_RLSHoldsWithoutTheFilter(t *testing.T) {
	f := newSiteFixture(t)
	for range 3 {
		f.exec(`INSERT INTO employees (tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF Staff', 'active', now())`,
			f.kf, "kf-"+uuid.NewString()[:8])
	}
	shipped := directorySQL(t, "listDirectoryEmployees")
	probe := strings.Replace(shipped, "WHERE e.tenant_id = $1", "WHERE ($1::uuid IS NOT NULL OR true)", 1)
	if probe == shipped {
		t.Fatal("PREMISE: the shipped statement's tenant predicate was not found")
	}
	read := func(q interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}) []uuid.UUID {
		rows, err := q.Query(f.ctx, probe, f.kf, directoryOrigin, maxUUID, int32(1000000))
		if err != nil {
			t.Fatal(err)
		}
		ids, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (uuid.UUID, error) {
			vals, err := r.Values()
			if err != nil {
				return uuid.Nil, err
			}
			return uuid.UUID(vals[0].([16]byte)), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	whose := func(ids []uuid.UUID) (kf, km, other int) {
		if err := f.owner.QueryRow(f.ctx, `SELECT count(*) FILTER (WHERE tenant_id = $2), count(*) FILTER (WHERE tenant_id = $3),
		                                          count(*) FILTER (WHERE tenant_id <> ALL (ARRAY[$2, $3]::uuid[]))
		                                     FROM employees WHERE id = ANY($1)`, ids, f.kf, f.km).Scan(&kf, &km, &other); err != nil {
			t.Fatal(err)
		}
		return
	}
	var app []uuid.UUID
	if err := f.data.WithTenant(f.ctx, f.km, func(ctx context.Context, tx pgx.Tx) error {
		app = read(tx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if kf, km, other := whose(app); kf != 0 || other != 0 || km < 1 {
		t.Errorf("in KM's context, asked for KF's people with the predicate neutralised, the statement read KF %d, KM %d, others %d; want KM's alone", kf, km, other)
	}
	if kf, km, _ := whose(read(f.owner)); kf != 3 || km < 1 {
		t.Errorf("CONTROL: the owner read KF %d, KM %d; want KF's three and KM's", kf, km)
	}
}

// TestEmployeeDirectory_WalksMissNothingUnderConcurrentWriters is the walk guarantee (the header of
// employeedirectory.go; K-21; plan §2 item 19) measured: eight writers make 60 changes each -- a new
// person or a renamed one, in KF, KM or an outsider C, each transaction holding its write 0-40 ms
// before it commits, a tenth rolled back, 0-20 ms between two -- while KM's reader walks its
// directory again and again, each walk from the previous one's asOf, pages of 1-200. Short walks on
// purpose: a write open across a walk's START is what the overlap exists for, and a long walk reaches
// the end of the order after such a write has committed, so it would prove less. The overlap is
// scaled to the holds (120 ms for 40 ms; the product's is ten minutes for a 30 s request). After the
// writers stop, one more walk: the reader's view (entries overwritten by id) is then EXACTLY the
// owner's KF and KM staff, every field, and never one of C's. Repeats are counted, not refused.
//
// MUTANT (measured, ADR 0028's API-8 note, on a database of 1 800 tenants -- CI's size): the overlap 0
// -- 3/3 red, 56-62 people missed and 20-26 stale a run. ⚠️ A member's page costs the peer set (T124's
// member half: holding_peer over every tenant), so on a database of tens of thousands of tenants the
// reader walks only a few times while the writers write, and the test proves less there (it stays
// correct: the final walk still has to find every change). (The second way to lose one -- asOf read at a later page instead of the walk's first --
// needs a walk longer than the overlap that ends while a write is open across it:
// TestEmployeeDirectory_TheWalksAsOfIsItsFirstPages builds exactly that.)
func TestEmployeeDirectory_WalksMissNothingUnderConcurrentWriters(t *testing.T) {
	f := newSiteFixture(t)
	c := uuid.New()
	f.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'api8 outsider', $2, 'restaurant', 'multi')`,
		c, "VAT-API8-C-"+c.String())
	f.exec(`UPDATE tenants SET api_code = 'KF' WHERE id = $1`, f.kf)
	f.exec(`UPDATE tenants SET api_code = 'KM' WHERE id = $1`, f.km)
	dir, err := NewEmployeeDirectory(f.data)
	if err != nil {
		t.Fatal(err)
	}
	dir.overlap = 120 * time.Millisecond
	dir.bound = time.Minute // a member's page costs the peer set; the bound is TestAPIRead_EveryReadCarriesTheBound's
	const writers, rounds, hold, pause = 8, 60, 40 * time.Millisecond, 20 * time.Millisecond
	tenants := []uuid.UUID{f.kf, f.km, c}
	ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	people := map[uuid.UUID][]uuid.UUID{}
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(i), 8))
			for r := range rounds {
				tenantID := tenants[rng.IntN(len(tenants))]
				mu.Lock()
				known := people[tenantID]
				var target uuid.UUID
				if len(known) > 0 && rng.IntN(2) == 0 {
					target = known[rng.IntN(len(known))]
				}
				mu.Unlock()
				rollback := rng.IntN(10) == 0
				var born uuid.UUID
				err := f.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
					name := fmt.Sprintf("W%d-%d", i, r)
					if target == uuid.Nil {
						if err := tx.QueryRow(ctx, `INSERT INTO employees (tenant_id, external_ref, first_name, last_name, full_name, status, invited_at)
						                            VALUES ($1, $2, $3, 'Stress', $3 || ' Stress', 'active', now()) RETURNING id`,
							tenantID, "st-"+uuid.NewString()[:13], name).Scan(&born); err != nil {
							return err
						}
					} else if _, err := tx.Exec(ctx, `UPDATE employees SET first_name = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, target, name); err != nil {
						return err
					}
					time.Sleep(time.Duration(rng.Int64N(int64(hold))))
					if rollback {
						return errRolledBack
					}
					return nil
				})
				switch {
				case errors.Is(err, errRolledBack):
				case err != nil:
					errs <- err
					return
				case born != uuid.Nil:
					mu.Lock()
					people[tenantID] = append(people[tenantID], born)
					mu.Unlock()
				}
				time.Sleep(time.Duration(rng.Int64N(int64(pause))))
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	view := map[uuid.UUID]DirectoryEntry{}
	walks, pages, repeats := 0, 0, 0
	rng := rand.New(rand.NewPCG(99, 8))
	var since *time.Time
	walk := func() {
		var q DirectoryQuery
		q.Since = since
		for {
			p, err := dir.DirectoryPage(ctx, f.km, q, 1+rng.IntN(200))
			if err != nil {
				t.Fatalf("walk %d: %v", walks, err)
			}
			pages++
			for _, e := range p.Entries {
				if _, seen := view[e.EmployeeID]; seen {
					repeats++
				}
				view[e.EmployeeID] = e
			}
			if !p.HasMore {
				asOf := p.AsOf
				since = &asOf
				walks++
				return
			}
			q.After, q.AsOf = p.Next, p.AsOf
		}
	}
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			walk()
		}
	}
	close(errs)
	for err := range errs {
		t.Fatalf("a writer: %v", err)
	}
	walk()

	truth := map[uuid.UUID]DirectoryEntry{}
	rows, err := f.owner.Query(f.ctx, `SELECT e.id, e.tenant_id, t.api_code, e.external_ref, e.employee_code, e.first_name, e.last_name,
	                                          e.status <> 'deactivated', e.updated_at
	                                     FROM employees e JOIN tenants t ON t.id = e.tenant_id WHERE e.tenant_id = ANY($1)`, []uuid.UUID{f.kf, f.km, c})
	if err != nil {
		t.Fatal(err)
	}
	outsider := map[uuid.UUID]bool{}
	for rows.Next() {
		var e DirectoryEntry
		var tenantID uuid.UUID
		if err := rows.Scan(&e.EmployeeID, &tenantID, &e.Company, &e.ExternalRef, &e.EmployeeCode, &e.FirstName, &e.LastName, &e.Active, &e.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		if tenantID == c {
			outsider[e.EmployeeID] = true
			continue
		}
		truth[e.EmployeeID] = e
	}
	rows.Close()
	missed, stale := 0, 0
	for id, want := range truth {
		got, ok := view[id]
		switch {
		case !ok:
			missed++
		case directoryEntryText(got) != directoryEntryText(want):
			stale++
		}
	}
	leaked := 0
	for id := range view {
		if outsider[id] {
			leaked++
		}
	}
	t.Logf("%d walks, %d pages, %d people of KF and KM, %d repeats; missed %d, stale %d, outsider's %d", walks, pages, len(truth), repeats, missed, stale, leaked)
	if missed+stale+leaked > 0 || len(truth) < 100 || walks < 2 {
		t.Errorf("the reader's view: %d missed, %d stale, %d of the outsider's (over %d people, %d walks)", missed, stale, leaked, len(truth), walks)
	}
	if len(view) != len(truth) {
		t.Errorf("the reader holds %d people, the two businesses %d", len(view), len(truth))
	}
}

// TestEmployeeDirectory_TheWalksAsOfIsItsFirstPages: asOf is the clock of the walk's FIRST page, carried
// by every later page -- not the page's own. The case it decides, built step by step (overlap 50 ms):
// KM's walk reads its first page (one entry); a KF person X is renamed in a transaction that stays
// open; a KF person Y is renamed and committed after it (so the walk passes X's new place while X is
// invisible); 150 ms pass -- longer than the overlap; the walk reads on to its end; X commits. The next
// walk, from updatedSince = the first walk's asOf, answers X with its new name. Every page of the first
// walk carried the same asOf, and it is no later than X's write.
//
// MUTANT (measured): asOf read at each page (the last page's handed out) -- the next walk starts
// after X's place and X's rename is never answered, red.
func TestEmployeeDirectory_TheWalksAsOfIsItsFirstPages(t *testing.T) {
	f := newSiteFixture(t)
	dir, err := NewEmployeeDirectory(f.data)
	if err != nil {
		t.Fatal(err)
	}
	dir.overlap = 50 * time.Millisecond
	var x, y uuid.UUID
	for _, p := range []*uuid.UUID{&x, &y} {
		if err := f.owner.QueryRow(f.ctx, `INSERT INTO employees (tenant_id, external_ref, first_name, last_name, full_name, status, invited_at)
		                                   VALUES ($1, $2, 'Before', 'Walk', 'Before Walk', 'active', now()) RETURNING id`,
			f.kf, "aw-"+uuid.NewString()[:8]).Scan(p); err != nil {
			t.Fatal(err)
		}
	}
	first, err := dir.DirectoryPage(f.ctx, f.km, DirectoryQuery{}, 1)
	if err != nil || !first.HasMore {
		t.Fatalf("the first page: %+v %v", first, err)
	}
	tx, err := f.owner.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, err := tx.Exec(f.ctx, `UPDATE employees SET first_name = 'Late' WHERE id = $1`, x); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE employees SET first_name = 'Early' WHERE id = $1`, y)
	time.Sleep(150 * time.Millisecond)
	q := DirectoryQuery{After: first.Next, AsOf: first.AsOf}
	for {
		p, err := dir.DirectoryPage(f.ctx, f.km, q, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !p.AsOf.Equal(first.AsOf) {
			t.Fatalf("a later page of the walk carries asOf %v, the first page %v", p.AsOf, first.AsOf)
		}
		for _, e := range p.Entries {
			if e.EmployeeID == x && *e.FirstName == "Late" {
				t.Fatal("PREMISE: the uncommitted rename was read")
			}
		}
		if !p.HasMore {
			break
		}
		q.After = p.Next
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var wrote time.Time
	if err := f.owner.QueryRow(f.ctx, `SELECT updated_at FROM employees WHERE id = $1`, x).Scan(&wrote); err != nil {
		t.Fatal(err)
	}
	if wrote.Before(first.AsOf) {
		t.Fatalf("PREMISE: X was written at %v, before the walk's asOf %v", wrote, first.AsOf)
	}
	since := first.AsOf
	next, err := dir.DirectoryPage(f.ctx, f.km, DirectoryQuery{Since: &since}, DirectoryMaxPage)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range next.Entries {
		if e.EmployeeID == x && *e.FirstName == "Late" {
			return
		}
	}
	t.Errorf("the rename committed after the walk passed it (written %v; the walk's asOf %v) is not in the next walk", wrote, first.AsOf)
}

// ownDirectoryPlan runs stmt (ListDirectoryEmployees' shape: $1 tenant_id, $2 after_at, $3 after_id,
// $4 page_size) as a GENERIC prepared statement -- the plan pgx's statement cache settles on -- as
// tappa_app in tenant's context at the position (at, id), sequential and bitmap scans and sorts priced
// out (the claim is about the text, not the planner's pick for a small table), and returns EXPLAIN
// ANALYZE's text.
func ownDirectoryPlan(t *testing.T, f *siteFixture, tenantID uuid.UUID, stmt string, at time.Time, id uuid.UUID) string {
	t.Helper()
	var plan []string
	if err := f.data.WithTenant(f.ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, s := range []string{
			`SET LOCAL plan_cache_mode = force_generic_plan`, `SET LOCAL enable_seqscan = off`, `SET LOCAL enable_bitmapscan = off`,
			`SET LOCAL enable_sort = off`, `SET LOCAL jit = off`,
			`PREPARE api8_own_plan(uuid, timestamptz, uuid, integer) AS ` + stmt,
		} {
			if _, err := tx.Exec(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		defer func() { _, _ = tx.Conn().Exec(context.Background(), `DEALLOCATE api8_own_plan`) }()
		rows, err := tx.Query(ctx, fmt.Sprintf(`EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) EXECUTE api8_own_plan('%s', '%s'::timestamptz, '%s', 101)`,
			tenantID, at.UTC().Format(time.RFC3339Nano), id))
		if err != nil {
			return err
		}
		plan, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

// TestEmployeeDirectory_TheBoundIsTheIndexsNotAFilter: the own read's position is an index condition
// of employees_tenant_updated_idx in a GENERIC plan (directory.sql's 🔴 note), so a page 2 500 rows
// into a business of 3 000 reads the page -- 101 rows -- and no filter removes a row. CONTROL (the
// measured mutant, API-8: 150 001 rows removed, 90 ms a page in a business of two hundred thousand rows): the same statement
// with the bound written coalesce($2, '-infinity') -- the position becomes a filter over the business's
// first 2 500 rows, red.
func TestEmployeeDirectory_TheBoundIsTheIndexsNotAFilter(t *testing.T) {
	f := newSiteFixture(t)
	f.exec(`INSERT INTO employees (tenant_id, external_ref, full_name, status, invited_at)
	        SELECT $1, 'bd-' || gen_random_uuid(), 'Bound Staff', 'active', now() FROM generate_series(1, 3000)`, f.kf)
	var at time.Time
	var id uuid.UUID
	if err := f.owner.QueryRow(f.ctx, `SELECT updated_at, id FROM employees WHERE tenant_id = $1 ORDER BY updated_at, id OFFSET 2500 LIMIT 1`, f.kf).Scan(&at, &id); err != nil {
		t.Fatal(err)
	}
	findings := func(plan string) []string {
		var out []string
		if !strings.Contains(plan, "using employees_tenant_updated_idx on employees") {
			out = append(out, "the read is not a walk of employees_tenant_updated_idx")
		}
		if !strings.Contains(plan, "Index Cond: ((tenant_id = $1) AND (ROW(updated_at, id) > ROW($2, $3)))") {
			out = append(out, "the position is not an index condition")
		}
		if strings.Contains(plan, "Rows Removed by Filter") {
			out = append(out, "a filter removed rows the walk read")
		}
		return out
	}
	shipped := directorySQL(t, "listDirectoryEmployees")
	plan := ownDirectoryPlan(t, f, f.kf, shipped, at, id)
	for _, x := range findings(plan) {
		t.Errorf("%s:\n%s", x, plan)
	}
	const bound = "($2::timestamptz, $3::uuid)"
	if strings.Count(shipped, bound) != 1 {
		t.Fatal("PREMISE: the shipped bound not found")
	}
	mutant := strings.Replace(shipped, bound, "(coalesce($2::timestamptz, '-infinity'::timestamptz), $3::uuid)", 1)
	if got := findings(ownDirectoryPlan(t, f, f.kf, mutant, at, id)); !slices.Contains(got, "a filter removed rows the walk read") {
		t.Errorf("CONTROL: the coalesce bound was not flagged as a filter: %v", got)
	}
}

var errRolledBack = errors.New("rolled back on purpose")

// directoryEntryText is every field of an entry, the pointers followed.
func directoryEntryText(e DirectoryEntry) string {
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%v|%d", e.EmployeeID, s(e.Company), s(e.ExternalRef), s(e.EmployeeCode), s(e.FirstName), s(e.LastName),
		e.Active, e.UpdatedAt.UnixMicro())
}
