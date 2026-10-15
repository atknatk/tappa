package tenant

// punchfeed_db_test.go -- M11 API-7: the feed reader's window and its cursor across a counter gap
// (ADR 0028 §5), against real Postgres with the shipped triggers. The HTTP contract, the four shapes,
// the outsider and the concurrent readers are internal/handler/api's punches_db_test.go.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/httpx"
)

// TestPunchFeed_TheWindowBoundsASparseScopeAndAGapIsCrossed: KF's feed is one own tap, seven taps of
// KM's employee on KF's plaques (KF's location scope only) and one more own tap. With a window of
// three positions, KF's employer scope reads the first tap, then an EMPTY page that still says
// hasMore, then the last tap -- each page touched at most three positions, the cursor always moved
// forward, nothing was skipped. The location scope, two at a time, reads all nine in order (an own tap is in both scopes).
// Then the restore runbook's step (ADR 0028 §5: every counter moved a million ahead): a reader at
// the old end -- or anywhere in the gap -- is caught up AT THE NEW COUNTER (never refused, never
// left behind); the next tap, past the gap, is one page away; a position past the counter is
// ErrCursorAhead.
func TestPunchFeed_TheWindowBoundsASparseScopeAndAGapIsCrossed(t *testing.T) {
	f := newSiteFixture(t)
	feed, err := NewPunchFeed(f.data)
	if err != nil {
		t.Fatal(err)
	}
	feed.window = 3
	venue := f.venue("KF feed venue")
	uid := f.plaque(venue)
	kfEmp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-FEED', 'KF Staff', 'active', now())`, kfEmp, f.kf)
	own := func() {
		t.Helper()
		if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                        VALUES ($1, $2, $3, $4, now(), 'ok', 'nfc', 'in')`, f.kf, kfEmp, venue, uid)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	own()
	for range 7 {
		f.tap(venue, uid, "ok", time.Now())
	}
	own()

	type step struct {
		seqs []int64
		next int64
		more bool
	}
	walk := func(role FeedRole, limit int, from int64) []step {
		t.Helper()
		var out []step
		for range 100 {
			p, err := feed.PunchPage(f.ctx, f.kf, role, from, limit)
			if err != nil {
				t.Fatal(err)
			}
			s := step{next: p.Next, more: p.HasMore}
			for _, e := range p.Events {
				s.seqs = append(s.seqs, e.Seq)
			}
			out = append(out, s)
			if p.Next < from {
				t.Fatalf("the cursor went back: %d after %d", p.Next, from)
			}
			from = p.Next
			if !p.HasMore {
				return out
			}
		}
		t.Fatal("the feed did not end")
		return nil
	}
	got := walk(FeedEmployer, 10, 0)
	want := []step{{[]int64{1}, 3, true}, {nil, 6, true}, {[]int64{9}, 9, false}}
	if len(got) != len(want) {
		t.Fatalf("employer scope: %+v, want %+v", got, want)
	}
	for i := range want {
		if len(got[i].seqs) != len(want[i].seqs) || got[i].next != want[i].next || got[i].more != want[i].more ||
			(len(want[i].seqs) == 1 && got[i].seqs[0] != want[i].seqs[0]) {
			t.Fatalf("employer scope page %d: %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
	var location []int64
	for _, s := range walk(FeedLocation, 2, 0) {
		location = append(location, s.seqs...)
	}
	for i, s := range location {
		if s != int64(i+1) || len(location) != 9 {
			t.Fatalf("location scope: %v, want 1..9 (KF's own taps are in both scopes)", location)
		}
	}

	f.exec(`UPDATE feed_counters SET last_seq = last_seq + 1000000 WHERE tenant_id = $1`, f.kf)
	for _, from := range []int64{9, 500000, 1000009} {
		p, err := feed.PunchPage(f.ctx, f.kf, FeedEmployer, from, 10)
		if err != nil || len(p.Events) != 0 || p.Next != 1000009 || p.HasMore {
			t.Fatalf("from %d past the restore step: %+v %v, want caught up at 1000009", from, p, err)
		}
	}
	own()
	p, err := feed.PunchPage(f.ctx, f.kf, FeedEmployer, 9, 10)
	if err != nil || len(p.Events) != 1 || p.Events[0].Seq != 1000010 || p.Next != 1000010 || p.HasMore {
		t.Fatalf("the tap past the gap: %+v %v", p, err)
	}
	if _, err := feed.PunchPage(f.ctx, f.kf, FeedEmployer, 1000011, 10); !errors.Is(err, ErrCursorAhead) {
		t.Fatalf("a position past the counter: %v, want ErrCursorAhead", err)
	}
	for _, bad := range []struct {
		role  FeedRole
		after int64
		limit int
	}{{"both", 0, 1}, {FeedEmployer, -1, 1}, {FeedEmployer, 0, 0}} {
		if _, err := feed.PunchPage(f.ctx, f.kf, bad.role, bad.after, bad.limit); err == nil || errors.Is(err, ErrCursorAhead) ||
			!strings.HasPrefix(err.Error(), "tenant: ") {
			t.Errorf("%+v: %v, want a refusal", bad, err)
		}
	}
}

// TestPunchHistory_TheWindowBoundsASparseScope (plan §5 API-7b: "seyrek kapsamda punch_time
// aralığında sınırsız yürüyüş olmaz"): in KF's range, one own tap, seven taps of KM's employee on
// KF's plaque (KF's location scope only) and one more own tap, declared an hour apart. With a window of
// three positions KF's employer walk reads the first tap, then an EMPTY page that still says hasMore,
// then the last tap -- each page touched at most three positions of the range, the position always
// moved forward in (punch_time, seq), and the walk's S0 never changed. The location walk, two at a
// time, reads all nine in punch_time order. A position outside the range is ErrHistoryRange (one
// past its S0 is not: the window counts every position -- API-7b review G-ORTA); an S0 past the
// counter is ErrCursorAhead.
func TestPunchHistory_TheWindowBoundsASparseScope(t *testing.T) {
	f := newSiteFixture(t)
	feed, err := NewPunchFeed(f.data)
	if err != nil {
		t.Fatal(err)
	}
	feed.window = 3
	venue := f.venue("KF history venue")
	uid := f.plaque(venue)
	kfEmp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-HIST', 'KF Staff', 'active', now())`, kfEmp, f.kf)
	t0 := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	own := func(at time.Time) {
		t.Helper()
		if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                        VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', 'in')`, f.kf, kfEmp, venue, uid, at)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	own(t0.Add(time.Hour))
	for i := range 7 {
		f.tap(venue, uid, "ok", t0.Add(time.Duration(i+2)*time.Hour))
	}
	own(t0.Add(9 * time.Hour))
	from, to := t0, t0.Add(24*time.Hour)

	type step struct {
		n    int
		next HistoryPosition
		more bool
	}
	walk := func(role FeedRole, limit int) ([]PunchEvent, []step) {
		t.Helper()
		var events []PunchEvent
		var steps []step
		q := HistoryQuery{From: from, To: to, First: true}
		for range 100 {
			p, err := feed.HistoryPage(f.ctx, f.kf, role, q, limit)
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, p.Events...)
			steps = append(steps, step{len(p.Events), p.Next, p.HasMore})
			if !q.First && (p.Next.S0 != q.At.S0 || p.Next.Time.Before(q.At.Time) || (p.Next.Time.Equal(q.At.Time) && p.Next.Seq < q.At.Seq)) {
				t.Fatalf("the position went back or the snapshot moved: %+v after %+v", p.Next, q.At)
			}
			q = HistoryQuery{From: from, To: to, At: p.Next}
			if !p.HasMore {
				return events, steps
			}
		}
		t.Fatal("the walk did not end")
		return nil, nil
	}
	ev, steps := walk(FeedEmployer, 10)
	if len(ev) != 2 || len(steps) != 3 || steps[0].n != 1 || !steps[0].more || steps[1].n != 0 || !steps[1].more || steps[2].n != 1 || steps[2].more {
		t.Fatalf("employer walk: %d events over %+v; want 1, an empty page with hasMore, 1", len(ev), steps)
	}
	if !ev[0].PunchTime.Equal(t0.Add(time.Hour)) || !ev[1].PunchTime.Equal(t0.Add(9*time.Hour)) {
		t.Fatalf("employer walk: %v, %v", ev[0].PunchTime, ev[1].PunchTime)
	}
	ev, _ = walk(FeedLocation, 2)
	for i, e := range ev {
		if len(ev) != 9 || !e.PunchTime.Equal(t0.Add(time.Duration(i+1)*time.Hour)) {
			t.Fatalf("location walk: %d events, the %d-th at %v", len(ev), i, e.PunchTime)
		}
	}
	first, err := feed.HistoryPage(f.ctx, f.kf, FeedLocation, HistoryQuery{From: from, To: to, First: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	s0 := first.Next.S0
	for _, c := range []struct {
		at   HistoryPosition
		want error
	}{
		{HistoryPosition{S0: s0, Time: to}, ErrHistoryRange},
		{HistoryPosition{S0: s0, Time: from.Add(-time.Microsecond)}, ErrHistoryRange},
		{HistoryPosition{S0: s0 + 1, Time: from}, ErrCursorAhead},
	} {
		if _, err := feed.HistoryPage(f.ctx, f.kf, FeedLocation, HistoryQuery{From: from, To: to, At: c.at}, 1); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.at, err, c.want)
		}
	}
	for _, r := range [][2]time.Time{{from, from}, {to, from}, {from, from.Add(MaxHistoryRange + time.Microsecond)}} {
		if _, err := feed.HistoryPage(f.ctx, f.kf, FeedLocation, HistoryQuery{From: r[0], To: r[1], First: true}, 1); !errors.Is(err, ErrHistoryRange) {
			t.Errorf("range %v..%v: %v, want ErrHistoryRange", r[0], r[1], err)
		}
	}
}

// kfTieGroup gives KF n own events at ONE punch_time with the heap in the REVERSE of their feed order
// (the API package's tieGroup, for this package's fixture -- its comment says why the product's own
// heap is not relied on, and that the product can produce this order): n flags (no event), then as the owner in
// KF's context their events with seq counter+n down to counter+1 and the counter moved. Returns the
// seqs in feed order.
func (f *siteFixture) kfTieGroup(t *testing.T, emp, venue uuid.UUID, uid string, n int, at time.Time) []int64 {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                         VALUES ($1, $2, $3, $4, $5, 'flag', 'nfc', 'in') RETURNING id`, f.kf, emp, venue, uid, at).Scan(&ids[i])
		}); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := f.owner.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.kf.String()); err != nil {
		t.Fatal(err)
	}
	var base int64
	if err := tx.QueryRow(f.ctx, `SELECT last_seq FROM feed_counters WHERE tenant_id = $1 FOR UPDATE`, f.kf).Scan(&base); err != nil {
		t.Fatal(err)
	}
	for i := n - 1; i >= 0; i-- {
		if _, err := tx.Exec(f.ctx, `INSERT INTO punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction, employee_id, location_id, tag_uid)
		                             VALUES ($1, $2, $3, $1, '{employer,location}', $4, now(), 'nfc', 'in', $5, $6, $7)`,
			f.kf, base+int64(i)+1, ids[i], at, emp, venue, uid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(f.ctx, `UPDATE feed_counters SET last_seq = $2 WHERE tenant_id = $1`, f.kf, base+int64(n)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = base + int64(i) + 1
	}
	return out
}

// historyWalk walks role over [from, to) with the given page sizes in turn; it returns the seqs.
func historyWalk(t *testing.T, f *siteFixture, feed *PunchFeed, role FeedRole, from, to time.Time, limits []int) ([]int64, int) {
	t.Helper()
	var seqs []int64
	q := HistoryQuery{From: from, To: to, First: true}
	for i := range 10000 {
		p, err := feed.HistoryPage(f.ctx, f.kf, role, q, limits[i%len(limits)])
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Events {
			seqs = append(seqs, e.Seq)
		}
		q = HistoryQuery{From: from, To: to, At: p.Next}
		if !p.HasMore {
			return seqs, i + 1
		}
	}
	t.Fatal("the walk did not end")
	return nil, 0
}

// TestPunchHistory_ATieAcrossTheWindowsEndIsCutByTheFeedOrder (API-7b review, DÜŞÜK-1 with E-ORTA):
// 30 KF events at one instant, heap in the reverse of the feed order, with a window of four
// positions -- so the window's end falls inside the tie, again and again. Walked with page sizes 1-7
// and 50, the range answers the 30 in feed order, each once. MUTANT (measured): the page's bound
// without seq ((punch_time) <= (end_time) instead of (punch_time, seq) <= (end_time, end_seq)) --
// red.
func TestPunchHistory_ATieAcrossTheWindowsEndIsCutByTheFeedOrder(t *testing.T) {
	f := newSiteFixture(t)
	feed, err := NewPunchFeed(f.data)
	if err != nil {
		t.Fatal(err)
	}
	feed.window = 4
	venue := f.venue("KF tie venue")
	uid := f.plaque(venue)
	emp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-TIE', 'KF Staff', 'active', now())`, emp, f.kf)
	at := time.Now().UTC().Truncate(time.Second).Add(-72 * time.Hour)
	want := f.kfTieGroup(t, emp, venue, uid, 30, at)
	// Inside the group (after its 10th, the 4-window ends at its 14th): the window and the page each
	// read at most window + 1 rows -- the index's third column is the tie-break, so neither reads the
	// rest of the group to sort it (API-7b third round, DÜŞÜK-1: with (tenant_id, punch_time) alone a
	// page read the whole group -- 50 000 at one instant: 25 000-49 999 rows removed).
	from, to := at.Add(-time.Hour), at.Add(time.Hour)
	if n, plan := shippedReads(t, f, "getPunchHistoryWindow", at, want[9], f.kf, from, to, int32(5), int32(4)); n > 5 {
		t.Fatalf("the window statement read %d rows inside a group of 30 at a window of four:\n%s", n, plan)
	}
	if n, plan := shippedReads(t, f, "listPunchHistory", f.kf, want[29], from, to, at, want[9], at, want[13], "employer", int32(51)); n > 5 {
		t.Fatalf("the page statement read %d rows inside a group of 30 up to a window's end four on:\n%s", n, plan)
	}
	for _, limits := range [][]int{{1}, {2}, {3}, {4}, {5}, {6}, {7}, {50}, {1, 7, 3, 50}} {
		got, _ := historyWalk(t, f, feed, FeedEmployer, from, to, limits)
		if !slices.Equal(got, want) {
			t.Fatalf("page sizes %v: %v, want %v", limits, got, want)
		}
	}
}

// TestPunchHistory_AStaleSnapshotReadsAtMostTheWindow (API-7b review, G-ORTA): KF holds 20 events in
// the range; a walk resumed with a STALE S0 -- one at or below which none of them is -- reads, with a
// window of three positions, seven pages of at most three positions each (empty, hasMore true) and
// ends; with the snapshot's filter in the window (the reviewed shape) the first page read all 20 rows,
// found none and ended. The same through the database: the shipped window statement at a window of
// three examines at most four rows of punch_events -- window + 1 -- and removes none by a filter
// (EXPLAIN ANALYZE of the index walk, shippedReads; before the index carried seq, an Incremental
// Sort read one row more to close its last group).
// MUTANT (measured): seq <= S0 back in the window -- red.
func TestPunchHistory_AStaleSnapshotReadsAtMostTheWindow(t *testing.T) {
	f := newSiteFixture(t)
	feed, err := NewPunchFeed(f.data)
	if err != nil {
		t.Fatal(err)
	}
	feed.window = 3
	venue := f.venue("KF stale venue")
	uid := f.plaque(venue)
	emp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-STALE', 'KF Staff', 'active', now())`, emp, f.kf)
	t0 := time.Now().UTC().Truncate(time.Second).Add(-96 * time.Hour)
	var s0 int64 // the counter before the 20: a snapshot that holds none of them
	if err := f.owner.QueryRow(f.ctx, `SELECT last_seq FROM feed_counters WHERE tenant_id = $1`, f.kf).Scan(&s0); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                        VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', 'in')`, f.kf, emp, venue, uid, t0.Add(time.Duration(i)*time.Minute))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	from, to := t0, t0.Add(time.Hour)
	q := HistoryQuery{From: from, To: to, At: HistoryPosition{S0: s0, Time: from}}
	pages := 0
	for range 100 {
		p, err := feed.HistoryPage(f.ctx, f.kf, FeedEmployer, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(p.Events) != 0 || p.Next.S0 != s0 {
			t.Fatalf("page %d with the stale S0 %d: %d events, S0 %d", pages, s0, len(p.Events), p.Next.S0)
		}
		q.At = p.Next
		if !p.HasMore {
			break
		}
	}
	if pages != 7 {
		t.Fatalf("the stale walk took %d pages, want 7 (20 positions, three a page)", pages)
	}
	if n, plan := shippedReads(t, f, "getPunchHistoryWindow", from, int64(0), f.kf, from, to, int32(4), int32(3)); n > 4 {
		t.Fatalf("the window statement read %d rows of punch_events at a window of three (want <= 4):\n%s", n, plan)
	}
}

// shippedReads runs the SHIPPED statement name (the store's unexported constant, read from the
// generated file) under EXPLAIN ANALYZE in KF's context and returns the most rows any scan of
// punch_events produced, failing on a row a filter threw away. The planner's choice of scan is pinned
// first (SET LOCAL enable_seqscan / enable_bitmapscan = off, the 00050 emsali): the claim is the
// shape of the INDEX walk -- "a page reads at most its window" -- and on this suite's small,
// unanalysed tables the planner would otherwise read the whole table by a sequential scan, which is
// the right plan there and says nothing about the walk (the API-7b third round: a fresh database
// went red 3/3 on that plan, a shared one green only because other packages' rows grew the table).
// At a million events the planner picks the index walk by itself (the API-7b notes' EXPLAIN).
func shippedReads(t *testing.T, f *siteFixture, name string, args ...any) (int, string) {
	t.Helper()
	var plan []string
	if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off; SET LOCAL enable_bitmapscan = off`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) `+shippedSQL(name), args...)
		if err != nil {
			return err
		}
		plan, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	most := 0
	for _, line := range plan {
		if strings.Contains(line, "on punch_events") {
			if i := strings.Index(line, "rows="); i >= 0 {
				var n int
				if _, err := fmt.Sscanf(line[i:], "rows=%d", &n); err == nil && n > most {
					most = n
				}
			}
		}
		if strings.Contains(line, "Rows Removed by Filter") {
			t.Fatalf("%s filters rows it reads: %s\n%s", name, line, strings.Join(plan, "\n"))
		}
	}
	if most == 0 && !strings.Contains(strings.Join(plan, "\n"), "on punch_events") {
		t.Fatalf("%s: no scan of punch_events in the plan:\n%s", name, strings.Join(plan, "\n"))
	}
	return most, strings.Join(plan, "\n")
}

// shippedSQL is the store's constant name, read from the generated file (the store keeps it
// unexported), positional as the store sends it -- getPunchHistoryWindow: $1 after_time, $2
// after_seq, $3 tenant_id, $4 from_time, $5 to_time, $6 scan_size, $7 window_size; listPunchHistory:
// $1 tenant_id, $2 s0, $3 from_time, $4 to_time, $5 after_time, $6 after_seq, $7 end_time, $8
// end_seq, $9 role, $10 page_size.
func shippedSQL(name string) string {
	b, err := os.ReadFile(filepath.Join("..", "..", "store", "punchfeed.sql.go"))
	if err != nil {
		panic(err)
	}
	src := string(b)
	head := "const " + name + " = `"
	i := strings.Index(src, head)
	if i < 0 {
		panic("no " + name + " in internal/store/punchfeed.sql.go")
	}
	rest := src[i+len(head):]
	return rest[:strings.Index(rest, "`")]
}

// boundProbe is a Database that, after each read's work, asks the same transaction what its
// statement timeout is -- what the read's statements ran under.
type boundProbe struct {
	data Database
	seen []string
}

func (b *boundProbe) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		var v string
		if err := tx.QueryRow(ctx, `SHOW statement_timeout`).Scan(&v); err != nil {
			return err
		}
		b.seen = append(b.seen, v)
		return nil
	})
}

// TestAPIRead_EveryReadCarriesTheBound (API-7b review, G-ORTA's extra guard): each read the
// integration API makes -- the feed, the history query, the employee list, one employee by ref and
// by id -- runs its statements under APIReadTimeout (5 s), and two of them fit in the request's own
// deadline. The pool's default stays unbounded (the server's 0): the bound is the API read's alone.
// MUTANT (measured): the bound removed from one read -- red, naming it.
func TestAPIRead_EveryReadCarriesTheBound(t *testing.T) {
	if 2*APIReadTimeout > httpx.RequestTimeout {
		t.Fatalf("APIReadTimeout %v: two of them must fit in the request's deadline %v", APIReadTimeout, httpx.RequestTimeout)
	}
	f := newSiteFixture(t)
	probe := &boundProbe{data: f.data}
	feed, err := NewPunchFeed(probe)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := NewStaff(probe, f.trail, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reads := []struct {
		name string
		run  func() error
	}{
		{"PunchPage", func() error { _, err := feed.PunchPage(f.ctx, f.km, FeedEmployer, 0, 10); return err }},
		{"HistoryPage", func() error {
			_, err := feed.HistoryPage(f.ctx, f.km, FeedEmployer, HistoryQuery{From: now.Add(-time.Hour), To: now, First: true}, 10)
			return err
		}},
		{"ListAPIEmployees", func() error { _, err := staff.ListAPIEmployees(f.ctx, f.km, nil, 10); return err }},
		{"APIEmployeeByRef", func() error { _, err := staff.APIEmployeeByRef(f.ctx, f.km, "ext-"+f.emp.String()[:8]); return err }},
		{"APIEmployeeByID", func() error { _, err := staff.APIEmployeeByID(f.ctx, f.km, f.emp); return err }},
	}
	for _, r := range reads {
		before := len(probe.seen)
		if err := r.run(); err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if len(probe.seen) != before+1 || probe.seen[before] != "5s" {
			t.Errorf("%s ran under statement_timeout %v, want 5s", r.name, probe.seen[before:])
		}
	}
	if err := f.data.WithTenant(f.ctx, f.km, func(ctx context.Context, tx pgx.Tx) error {
		var v string
		if err := tx.QueryRow(ctx, `SHOW statement_timeout`).Scan(&v); err != nil {
			return err
		}
		if v != "0" {
			return fmt.Errorf("a plain transaction of the pool runs under %s, want the server's 0 (the bound is LOCAL)", v)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// shippedRowsUnsorted runs the SHIPPED statement name with every index scan off -- a sequential scan
// and a Sort decide the order -- and returns its rows' first column as int64 (or the row's columns
// for the window statement: scanned, end seq).
func shippedRowsUnsorted(t *testing.T, f *siteFixture, name string, scan func(pgx.Rows) ([]int64, error), args ...any) []int64 {
	t.Helper()
	var out []int64
	if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL enable_indexscan = off; SET LOCAL enable_indexonlyscan = off; SET LOCAL enable_bitmapscan = off`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, shippedSQL(name), args...)
		if err != nil {
			return err
		}
		out, err = scan(rows)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPunchHistory_TheTieBreakIsTheTextsNotThePlans (API-7b third round, DÜŞÜK-3): since the index
// carries seq, an index walk returns ties in seq order whatever the statement says -- a seq dropped
// from an ORDER BY would stay invisible to every test that runs the planner's choice. So the shipped
// statements run here with the index scans OFF (a sequential scan of a heap in the reverse of the
// feed order, then a Sort): the page answers a group of 30 ties in feed order, and the window's end
// four positions into the group is the group's 4th. MUTANTS (measured), each red: seq removed from
// the page's ORDER BY; from the window's; from the end's DESC.
func TestPunchHistory_TheTieBreakIsTheTextsNotThePlans(t *testing.T) {
	f := newSiteFixture(t)
	venue := f.venue("KF texts venue")
	uid := f.plaque(venue)
	emp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-TEXT', 'KF Staff', 'active', now())`, emp, f.kf)
	at := time.Now().UTC().Truncate(time.Second).Add(-120 * time.Hour)
	want := f.kfTieGroup(t, emp, venue, uid, 30, at)
	from, to := at.Add(-time.Hour), at.Add(time.Hour)
	page := shippedRowsUnsorted(t, f, "listPunchHistory", func(r pgx.Rows) ([]int64, error) {
		var seqs []int64
		for r.Next() {
			vals, err := r.Values()
			if err != nil {
				return nil, err
			}
			seqs = append(seqs, vals[0].(int64))
		}
		return seqs, r.Err()
	}, f.kf, want[29], from, to, from, int64(0), at, want[29], "employer", int32(31))
	if !slices.Equal(page, want) {
		t.Fatalf("the page under a Sort answered %v, want the feed order %v", page, want)
	}
	end := shippedRowsUnsorted(t, f, "getPunchHistoryWindow", func(r pgx.Rows) ([]int64, error) {
		defer r.Close()
		if !r.Next() {
			return nil, errors.New("no window row")
		}
		var scanned, endSeq int64
		var endTime time.Time
		if err := r.Scan(&scanned, &endTime, &endSeq); err != nil {
			return nil, err
		}
		return []int64{scanned, endSeq}, r.Err()
	}, from, int64(0), f.kf, from, to, int32(5), int32(4))
	if len(end) != 2 || end[0] != 5 || end[1] != want[3] {
		t.Fatalf("the window under a Sort: scanned %v, want 5 and the end at the group's 4th (%d)", end, want[3])
	}
}
