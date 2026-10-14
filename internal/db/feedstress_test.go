package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// feedstress_test.go -- M11 API-2: the cursor that never skips (ADR 0028 §5, K-13, K-31; plan §5
// API-2 (b) and (q)), measured with committing writers and a reader polling as they go.
//
// THE HARNESS (feedRun). ≥ 12 writers, each on its own connection, each transaction ONE event-
// writing row -- the product's shape (a tap is one INSERT per transaction, an approval one review
// per transaction; the counter-order argument needs it, ADR 0028 §5's "kilit değişmezi") --
// mixing own-tenant taps and holding taps in BOTH directions (A on B's plaque and B on A's, C on
// D's and D on C's), a tenth rolled back whole and a tenth written in a savepoint that is rolled
// back before a second row is written and committed; a short random pause before each commit
// widens the windows. One reader per tenant polls `seq > cursor ORDER BY seq` the whole time and
// keeps every seq it was handed. At the end, per tenant: every seq the feed holds was handed to
// the reader (0 missed), the seqs are 1..n (dense) and the events are exactly the committed
// rows' targets; and no writer met a deadlock (40P01) or a lock timeout.
//
// WHERE THE MUTANTS LIVE. A mutant of the real triggers is a CREATE OR REPLACE that every other
// package's tests would meet in the meantime, so the three designs ADR 0028 §9 measured and
// eliminated -- a global sequence with a cache, the counter raised in a separate transaction, two
// triggers locking the two counters -- and the pre-lock without its "make exist" step (K-31, the
// 7th round) run against a LAB: a scratch schema of three tables (rows, counters, events) with the
// design under test as its trigger, built and dropped by the test, driven by the SAME harness.
// The lab's control design is the real triggers' counter logic (and passes); each mutant is red
// on what the harness counts. The real triggers' own stress is feedRealDesign.

// feedDesign is what the harness drives.
type feedDesign struct {
	// write writes one row of employer's -- an own tap (site uuid.Nil) or a holding tap on site's
	// plaque -- in tx, and returns its id. seq allocates in a separate transaction where the
	// design needs it (nil otherwise).
	write func(ctx context.Context, tx pgx.Tx, w *feedWriter, employer, site uuid.UUID) (uuid.UUID, error)
	// read returns tenant's events after cursor, in seq order, on c.
	read func(ctx context.Context, c *pgx.Conn, tenant uuid.UUID, cursor int64) ([]int64, error)
	// final returns every (seq, punch) of tenant's feed, read as the owner.
	final func(ctx context.Context, c *pgx.Conn, tenant uuid.UUID) (map[int64]uuid.UUID, error)
}

// feedWriter is one writer goroutine's connections.
type feedWriter struct {
	c, side *pgx.Conn
}

// feedOutcome is what one run measured.
type feedOutcome struct {
	committed, events, missed, deadlocks, lockTimeouts int
	notDense, wrongEvents                              []string
	other                                              []error
}

func (o feedOutcome) String() string {
	return fmt.Sprintf("%d committed rows, %d events, %d missed, %d deadlocks, %d lock timeouts, not dense %v, wrong events %v, other %v",
		o.committed, o.events, o.missed, o.deadlocks, o.lockTimeouts, o.notDense, o.wrongEvents, o.other)
}

// feedRandN is a uniform int in [0, n).
func feedRandN(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(v.Int64())
}

// feedRun drives design with writers x rounds rows over the four tenants (pairs[i] are peers) and
// one reader per tenant, then judges the feeds.
func feedRun(t *testing.T, ctx context.Context, connect func() *pgx.Conn, design feedDesign, tenants [4]uuid.UUID, writers, rounds int) feedOutcome {
	t.Helper()
	peer := map[uuid.UUID]uuid.UUID{tenants[0]: tenants[1], tenants[1]: tenants[0], tenants[2]: tenants[3], tenants[3]: tenants[2]}
	var (
		mu       sync.Mutex
		out      feedOutcome
		expected = map[uuid.UUID]map[uuid.UUID]bool{} // target tenant -> punch ids it must hold
		seen     = map[uuid.UUID]map[int64]bool{}
	)
	for _, tn := range tenants {
		expected[tn], seen[tn] = map[uuid.UUID]bool{}, map[int64]bool{}
	}
	done := make(chan struct{})
	var readers sync.WaitGroup
	for _, tn := range tenants {
		c := connect()
		readers.Add(1)
		go func(tn uuid.UUID) {
			defer readers.Done()
			var cursor int64
			poll := func() {
				seqs, err := design.read(ctx, c, tn, cursor)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					out.other = append(out.other, fmt.Errorf("reader: %w", err))
					return
				}
				for _, s := range seqs {
					seen[tn][s] = true
					cursor = max(cursor, s)
				}
			}
			for {
				select {
				case <-done:
					poll()
					return
				default:
					poll()
					time.Sleep(time.Duration(1+feedRandN(4)) * time.Millisecond)
				}
			}
		}(tn)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		w := &feedWriter{c: connect(), side: connect()}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for r := 0; r < rounds; r++ {
				employer := tenants[(i+r)%4]
				site := uuid.Nil
				if feedRandN(2) == 0 {
					site = peer[employer]
				}
				id, committed, err := feedOneTx(ctx, design, w, employer, site)
				mu.Lock()
				switch code, _ := opCode(err); {
				case err == nil && committed:
					out.committed++
					expected[employer][id] = true
					if site != uuid.Nil {
						expected[site][id] = true
					}
				case err == nil:
				case code == sqlstateDeadlockDetected:
					out.deadlocks++
				case code == sqlstateLockNotAvailable:
					out.lockTimeouts++
				default:
					out.other = append(out.other, err)
				}
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(done)
	readers.Wait()

	owner := connect()
	for _, tn := range tenants {
		feed, err := design.final(ctx, owner, tn)
		if err != nil {
			t.Fatalf("read the final feed: %v", err)
		}
		seqs := make([]int64, 0, len(feed))
		for s, punch := range feed {
			seqs = append(seqs, s)
			if !seen[tn][s] {
				out.missed++
			}
			if !expected[tn][punch] {
				out.wrongEvents = append(out.wrongEvents, fmt.Sprintf("%v seq %d: a punch no committed row targets here", tn, s))
			}
		}
		out.events += len(feed)
		if len(feed) != len(expected[tn]) {
			out.wrongEvents = append(out.wrongEvents, fmt.Sprintf("%v: %d events for %d committed targets", tn, len(feed), len(expected[tn])))
		}
		slices.Sort(seqs)
		for i, s := range seqs {
			if s != int64(i+1) {
				out.notDense = append(out.notDense, fmt.Sprintf("%v: seq %d at position %d", tn, s, i+1))
				break
			}
		}
	}
	return out
}

// feedOneTx is one writer's transaction: a tenth rolled back whole, a tenth with a row rolled back
// to a savepoint before the committed one.
func feedOneTx(ctx context.Context, design feedDesign, w *feedWriter, employer, site uuid.UUID) (uuid.UUID, bool, error) {
	tx, err := w.c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return uuid.Nil, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// The deadlock detector's wait shortened for this transaction (the owner may; the default 1 s
	// made the two-trigger mutant's hundred deadlocks a minute and a half) -- detection, not
	// avoidance: a cycle is a cycle at any timeout.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '20s'; SET LOCAL deadlock_timeout = '100ms'`); err != nil {
		return uuid.Nil, false, err
	}
	mode := feedRandN(10)
	if mode == 0 {
		sp, err := tx.Begin(ctx)
		if err != nil {
			return uuid.Nil, false, err
		}
		if _, err := design.write(ctx, sp, w, employer, site); err != nil {
			return uuid.Nil, false, err
		}
		time.Sleep(time.Duration(feedRandN(2)) * time.Millisecond)
		if err := sp.Rollback(ctx); err != nil {
			return uuid.Nil, false, err
		}
	}
	id, err := design.write(ctx, tx, w, employer, site)
	if err != nil {
		return uuid.Nil, false, err
	}
	time.Sleep(time.Duration(feedRandN(3)) * time.Millisecond)
	if mode == 1 {
		return id, false, tx.Rollback(ctx)
	}
	return id, true, tx.Commit(ctx)
}

// ------------------------------------------------------------------ the real design --

// feedRealWorld is four committed tenants, A and B peers in one holding and C and D in another,
// each with a venue, an active plaque far ahead of any counter a row names and an employee.
type feedRealWorld struct {
	tenants [4]uuid.UUID
	venue   map[uuid.UUID]uuid.UUID
	tag     map[uuid.UUID]string
	emp     map[uuid.UUID]uuid.UUID
}

func feedRealWorldLive(t *testing.T, f *holdingLive) feedRealWorld {
	t.Helper()
	w := feedRealWorld{venue: map[uuid.UUID]uuid.UUID{}, tag: map[uuid.UUID]string{}, emp: map[uuid.UUID]uuid.UUID{}}
	for i, code := range []string{"FA", "FB", "FC", "FD"} {
		w.tenants[i] = f.tenant(t, "Europe/Malta", code)
	}
	h1, h2 := f.holding(t), f.holding(t)
	for i, tn := range w.tenants {
		f.member(t, []uuid.UUID{h1, h1, h2, h2}[i], tn, "10 minutes", "")
		var v uuid.UUID
		if err := f.owner.QueryRow(f.ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'api2 stress', 'ST-1') RETURNING id`, tn).Scan(&v); err != nil {
			t.Fatal(err)
		}
		w.venue[tn] = v
		w.tag[tn] = holdingNewTag(t, f.ctx, f.owner, tn, &v, "active", 1000000)
		w.emp[tn] = holdingNewEmployee(t, f.ctx, f.owner, tn, "active")
	}
	return w
}

// feedRealDesign drives the shipped triggers: rows written as tappa_app in the employer's
// context, the feed read as tappa_app in the target's (RLS).
func feedRealDesign(w feedRealWorld) feedDesign {
	return feedDesign{
		write: func(ctx context.Context, tx pgx.Tx, _ *feedWriter, employer, site uuid.UUID) (uuid.UUID, error) {
			if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`); err != nil {
				return uuid.Nil, err
			}
			if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, employer.String()); err != nil {
				return uuid.Nil, err
			}
			var id uuid.UUID
			var err error
			if site == uuid.Nil {
				err = tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
				                        VALUES ($1, $2, $3, $4, now(), 'ok', 'nfc', 'in') RETURNING id`,
					employer, w.emp[employer], w.venue[employer], w.tag[employer]).Scan(&id)
			} else {
				err = tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr,
				                                                  site_tenant_id, site_location_id, site_tag_uid)
				                        VALUES ($1, $2, now(), 'ok', 'nfc', 'in', true, 100, $3, $4, $5) RETURNING id`,
					employer, w.emp[employer], site, w.venue[site], w.tag[site]).Scan(&id)
			}
			return id, err
		},
		read: func(ctx context.Context, c *pgx.Conn, tenant uuid.UUID, cursor int64) ([]int64, error) {
			tx, err := c.Begin(ctx)
			if err != nil {
				return nil, err
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
				return nil, err
			}
			rows, err := tx.Query(ctx, `SELECT seq FROM punch_events WHERE tenant_id = $1 AND seq > $2 ORDER BY seq`, tenant, cursor)
			if err != nil {
				return nil, err
			}
			return pgx.CollectRows(rows, pgx.RowTo[int64])
		},
		final: func(ctx context.Context, c *pgx.Conn, tenant uuid.UUID) (map[int64]uuid.UUID, error) {
			return feedFinal(ctx, c, `SELECT seq, punch_id FROM punch_events WHERE tenant_id = $1`, tenant)
		},
	}
}

func feedFinal(ctx context.Context, c *pgx.Conn, sql string, tenant uuid.UUID) (map[int64]uuid.UUID, error) {
	rows, err := c.Query(ctx, sql, tenant)
	if err != nil {
		return nil, err
	}
	out := map[int64]uuid.UUID{}
	for rows.Next() {
		var s int64
		var p uuid.UUID
		if err := rows.Scan(&s, &p); err != nil {
			return nil, err
		}
		out[s] = p
	}
	return out, rows.Err()
}

// TestFeed_StressNoMissNoDeadlock is plan §5 API-2 (b) on the shipped triggers: 12 writers x 100
// rows over 4 committed tenants (two holdings, holding taps in both directions -- K-31's reverse
// pairs), a tenth aborted and a tenth with a savepoint rolled back, one polling reader per tenant
// as tappa_app. 0 missed, 0 deadlocks, 0 lock timeouts, every feed 1..n, the events exactly the
// committed rows' targets (one per own tap, two per holding tap).
func TestFeed_StressNoMissNoDeadlock(t *testing.T) {
	f := holdingLiveFixture(t)
	w := feedRealWorldLive(t, f)
	o := feedRun(t, f.ctx, func() *pgx.Conn { return f.connect(t, f.ctx) }, feedRealDesign(w), w.tenants, 12, 100)
	t.Logf("shipped triggers: %s", o)
	if o.missed != 0 || o.deadlocks != 0 || o.lockTimeouts != 0 || len(o.notDense) != 0 || len(o.wrongEvents) != 0 || len(o.other) != 0 {
		t.Errorf("shipped triggers: %s", o)
	}
	if o.committed < 800 || o.events <= o.committed {
		t.Errorf("anti-vacuity: %d committed rows, %d events", o.committed, o.events)
	}
}

// feedBurst starts writers transactions at once on a barrier, each ONE holding tap -- half A on
// B's plaque, half B on A's (and C/D the same) -- on tenants whose counter rows the caller removed:
// the first touch of each counter row is the race K-31's "make exist" step is for. It returns how
// many met a deadlock, and the other errors.
func feedBurst(t *testing.T, ctx context.Context, writers []*feedWriter, design feedDesign, tenants [4]uuid.UUID) (deadlocks int, other []error) {
	t.Helper()
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	pairs := [][2]uuid.UUID{{tenants[0], tenants[1]}, {tenants[1], tenants[0]}, {tenants[2], tenants[3]}, {tenants[3], tenants[2]}}
	for i, w := range writers {
		p := pairs[i%len(pairs)]
		wg.Add(1)
		go func(w *feedWriter) {
			defer wg.Done()
			<-start
			_, _, err := feedOneTxPlain(ctx, design, w, p[0], p[1])
			mu.Lock()
			defer mu.Unlock()
			switch code, _ := opCode(err); {
			case err == nil:
			case code == sqlstateDeadlockDetected:
				deadlocks++
			default:
				other = append(other, err)
			}
		}(w)
	}
	close(start)
	wg.Wait()
	return deadlocks, other
}

// feedWriters opens n writers' connections on f, once for a test (a burst per round reuses them).
func feedWriters(t *testing.T, f *holdingLive, n int) []*feedWriter {
	t.Helper()
	var out []*feedWriter
	for i := 0; i < n; i++ {
		out = append(out, &feedWriter{c: f.connect(t, f.ctx)})
	}
	return out
}

// feedOneTxPlain is one committed transaction of one row, no abort and no savepoint.
func feedOneTxPlain(ctx context.Context, design feedDesign, w *feedWriter, employer, site uuid.UUID) (uuid.UUID, bool, error) {
	tx, err := w.c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return uuid.Nil, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '20s'; SET LOCAL deadlock_timeout = '100ms'`); err != nil {
		return uuid.Nil, false, err
	}
	id, err := design.write(ctx, tx, w, employer, site)
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, tx.Commit(ctx)
}

// feedMissingRounds is how many fresh worlds TestFeed_MissingCounterRowsNoDeadlock and its mutant
// race on.
const feedMissingRounds = 12

// TestFeed_MissingCounterRowsNoDeadlock is plan §5 API-2 (q) on the shipped triggers, the 7th
// round's shape: feedMissingRounds times, four fresh committed tenants (two holdings) whose
// counter rows are DELETED (a tenant the tenants trigger never saw -- a restore that lost the row),
// then 12 holding taps in both directions started at once on a barrier -- every one the first touch
// of its two counter rows. holding_feed's "make exist" step opens the rows in canonical order and
// its one locking statement takes both: 0 deadlocks, every feed 1..n, every counter row there
// again. (Without the step: TestFeedMutant_PreLockWithoutMakeExistDeadlocks; on the shipped
// function with the step removed, measured in a throwaway container -- the API-2 note.)
func TestFeed_MissingCounterRowsNoDeadlock(t *testing.T) {
	f := holdingLiveFixture(t)
	writers := feedWriters(t, f, 12)
	deadlocks, events := 0, int64(0)
	for round := 0; round < feedMissingRounds; round++ {
		w := feedRealWorldLive(t, f)
		if _, err := f.owner.Exec(f.ctx, `DELETE FROM feed_counters WHERE tenant_id = ANY($1)`, w.tenants[:]); err != nil {
			t.Fatal(err)
		}
		d, other := feedBurst(t, f.ctx, writers, feedRealDesign(w), w.tenants)
		deadlocks += d
		for _, e := range other {
			t.Errorf("round %d: %v", round, e)
		}
		if n := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM feed_counters WHERE tenant_id = ANY($1)`, w.tenants[:]); n != 4 {
			t.Errorf("round %d: %d of the four counter rows exist after the burst", round, n)
		}
		if n := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM (SELECT tenant_id FROM punch_events WHERE tenant_id = ANY($1) GROUP BY tenant_id
		                                   HAVING min(seq) <> 1 OR max(seq) <> count(*)) d`, w.tenants[:]); n != 0 {
			t.Errorf("round %d: %d feeds are not 1..n", round, n)
		}
		events += opInt(t, f.ctx, f.owner, `SELECT count(*) FROM punch_events WHERE tenant_id = ANY($1)`, w.tenants[:])
	}
	t.Logf("shipped triggers, counter rows missing: %d rounds x 12 holding taps, %d events, %d deadlocks", feedMissingRounds, events, deadlocks)
	if deadlocks != 0 {
		t.Errorf("%d deadlocks on missing counter rows -- K-31's make-exist step does not hold", deadlocks)
	}
	if events < int64(feedMissingRounds)*12*2-int64(deadlocks)*2 || events == 0 {
		t.Errorf("anti-vacuity: %d events", events)
	}
}

// ------------------------------------------------------------------------ the lab --

// feedLab is a scratch schema: rows (tx), counters, events and a global sequence with a cache,
// the design's trigger on tx. Built committed (the writers are other sessions), dropped at the end.
type feedLab struct{ schema string }

func newFeedLab(t *testing.T, f *holdingLive, body string, extra ...string) feedLab {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	l := feedLab{schema: "zz_api2_lab_" + hex.EncodeToString(b)}
	q := func(s string) string { return fmt.Sprintf(s, l.schema) }
	t.Cleanup(func() {
		if _, err := f.owner.Exec(context.Background(), q(`DROP SCHEMA %[1]s CASCADE`)); err != nil {
			t.Errorf("drop the lab: %v", err)
		}
	})
	stmts := []string{
		q(`CREATE SCHEMA %[1]s`),
		q(`CREATE TABLE %[1]s.counters (tenant_id uuid PRIMARY KEY, last_seq bigint NOT NULL DEFAULT 0)`),
		q(`CREATE TABLE %[1]s.events (tenant_id uuid NOT NULL, seq bigint NOT NULL, punch_id uuid NOT NULL, PRIMARY KEY (tenant_id, seq))`),
		q(`CREATE TABLE %[1]s.tx (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, site_tenant_id uuid,
		                          seq_hint bigint, site_seq_hint bigint)`),
		q(`CREATE SEQUENCE %[1]s.global_seq CACHE 20`),
		fmt.Sprintf(`CREATE FUNCTION %[1]s.feed() RETURNS trigger LANGUAGE plpgsql SET search_path = %[1]s, pg_catalog AS $$
			DECLARE s bigint; n integer;
			BEGIN %[2]s RETURN NULL; END $$`, l.schema, body),
		q(`CREATE TRIGGER a_feed AFTER INSERT ON %[1]s.tx FOR EACH ROW EXECUTE FUNCTION %[1]s.feed()`),
	}
	for _, s := range extra {
		stmts = append(stmts, strings.ReplaceAll(s, "%[1]s", l.schema))
	}
	for _, s := range stmts {
		if _, err := f.owner.Exec(f.ctx, s); err != nil {
			t.Fatalf("build the lab: %v\n%s", err, s)
		}
	}
	return l
}

// design drives the lab as the owner; sideAlloc allocates the seqs in a separate, committed
// transaction (the counter-in-a-separate-transaction mutant) before the row is written.
func (l feedLab) design(sideAlloc bool) feedDesign {
	return feedDesign{
		write: func(ctx context.Context, tx pgx.Tx, w *feedWriter, employer, site uuid.UUID) (uuid.UUID, error) {
			var hint, siteHint *int64
			if sideAlloc {
				for _, tn := range []uuid.UUID{employer, site} {
					if tn == uuid.Nil {
						continue
					}
					var s int64
					if err := w.side.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %s.counters AS c VALUES ($1, 1) ON CONFLICT (tenant_id)
					                                            DO UPDATE SET last_seq = c.last_seq + 1 RETURNING c.last_seq`, l.schema), tn).Scan(&s); err != nil {
						return uuid.Nil, err
					}
					if tn == employer {
						hint = &s
					} else {
						siteHint = &s
					}
				}
				time.Sleep(time.Duration(feedRandN(3)) * time.Millisecond)
			}
			var s any
			if site != uuid.Nil {
				s = site
			}
			var id uuid.UUID
			err := tx.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %s.tx (tenant_id, site_tenant_id, seq_hint, site_seq_hint) VALUES ($1, $2, $3, $4) RETURNING id`, l.schema),
				employer, s, hint, siteHint).Scan(&id)
			return id, err
		},
		read: func(ctx context.Context, c *pgx.Conn, tenant uuid.UUID, cursor int64) ([]int64, error) {
			rows, err := c.Query(ctx, fmt.Sprintf(`SELECT seq FROM %s.events WHERE tenant_id = $1 AND seq > $2 ORDER BY seq`, l.schema), tenant, cursor)
			if err != nil {
				return nil, err
			}
			return pgx.CollectRows(rows, pgx.RowTo[int64])
		},
		final: func(ctx context.Context, c *pgx.Conn, tenant uuid.UUID) (map[int64]uuid.UUID, error) {
			return feedFinal(ctx, c, fmt.Sprintf(`SELECT seq, punch_id FROM %s.events WHERE tenant_id = $1`, l.schema), tenant)
		},
	}
}

// The lab's trigger bodies. feedLabControl is the shipped counter logic (punch_feed for an own
// row; holding_feed's K-31 pre-lock for a holding row); the pause after the lock widens the window
// in which the mutants' second lock would be taken.
const (
	feedLabRaise = `INSERT INTO counters AS c VALUES (%s, 1) ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1 RETURNING c.last_seq INTO s;
		INSERT INTO events VALUES (%s, s, NEW.id);`
	feedLabMakeExist = `INSERT INTO counters VALUES (LEAST(NEW.tenant_id, NEW.site_tenant_id), 0), (GREATEST(NEW.tenant_id, NEW.site_tenant_id), 0)
		ON CONFLICT (tenant_id) DO NOTHING;`
	feedLabLock = `SELECT count(*) INTO n FROM (SELECT 1 FROM counters WHERE tenant_id IN (NEW.tenant_id, NEW.site_tenant_id)
		ORDER BY tenant_id FOR UPDATE) k;`
)

func feedLabControl(makeExist, assert bool) string {
	b := `IF NEW.site_tenant_id IS NULL THEN ` + fmt.Sprintf(feedLabRaise, "NEW.tenant_id", "NEW.tenant_id") + ` ELSE `
	if makeExist {
		b += feedLabMakeExist
	}
	b += feedLabLock
	if assert {
		b += ` IF n <> 2 THEN RAISE EXCEPTION 'locked %', n; END IF;`
	}
	b += ` PERFORM pg_sleep(0.002); ` + fmt.Sprintf(feedLabRaise, "NEW.tenant_id", "NEW.tenant_id") +
		fmt.Sprintf(feedLabRaise, "NEW.site_tenant_id", "NEW.site_tenant_id") + ` END IF;`
	return b
}

// feedLabTenants are four fresh ids for the lab (it has no tenants table).
func feedLabTenants() [4]uuid.UUID {
	return [4]uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
}

// feedLabRun builds a lab with body on f and drives it. One fixture per test: the operator-tables
// lock it takes is taken once per test tree (cmd/opadmin's TestTablesLock_IsTakenOncePerTestTree).
func feedLabRun(t *testing.T, f *holdingLive, body string, sideAlloc bool, seedCounters bool, extra ...string) feedOutcome {
	t.Helper()
	l := newFeedLab(t, f, body, extra...)
	tenants := feedLabTenants()
	if seedCounters {
		if _, err := f.owner.Exec(f.ctx, fmt.Sprintf(`INSERT INTO %s.counters SELECT unnest($1::uuid[]), 0`, l.schema), tenants[:]); err != nil {
			t.Fatal(err)
		}
	}
	return feedRun(t, f.ctx, func() *pgx.Conn { return f.connect(t, f.ctx) }, l.design(sideAlloc), tenants, 12, 40)
}

// TestFeedMutant_LabControlPasses: the lab with the shipped counter logic is green on the harness
// -- so a mutant's red below is the design's, not the lab's.
func TestFeedMutant_LabControlPasses(t *testing.T) {
	o := feedLabRun(t, holdingLiveFixture(t), feedLabControl(true, true), false, true)
	t.Logf("lab control: %s", o)
	if o.missed != 0 || o.deadlocks != 0 || o.lockTimeouts != 0 || len(o.notDense) != 0 || len(o.wrongEvents) != 0 || len(o.other) != 0 {
		t.Errorf("lab control: %s", o)
	}
}

// TestFeedMutant_UnlockedSequenceMisses: the events numbered by a global sequence with a cache
// (ADR 0028 §9: the 2nd round's design -- 1 016 of 1 234 events missed) -- no counter row, no lock.
// The reader misses events (a seq handed out earlier commits later, behind the cursor).
func TestFeedMutant_UnlockedSequenceMisses(t *testing.T) {
	o := feedLabRun(t, holdingLiveFixture(t), `s := nextval('global_seq'); INSERT INTO events VALUES (NEW.tenant_id, s, NEW.id);
		IF NEW.site_tenant_id IS NOT NULL THEN s := nextval('global_seq'); INSERT INTO events VALUES (NEW.site_tenant_id, s, NEW.id); END IF;`,
		false, true)
	t.Logf("unlocked sequence: %s", o)
	if o.missed == 0 {
		t.Errorf("the unlocked sequence missed nothing -- the harness cannot see the class it is for: %s", o)
	}
}

// TestFeedMutant_CounterInSeparateTransactionMisses: the counter raised and committed in its own
// transaction before the row's (the 3rd round's mutant -- 408 missed): the seq order is the
// allocation order, the commit order is not, and the reader misses events.
func TestFeedMutant_CounterInSeparateTransactionMisses(t *testing.T) {
	o := feedLabRun(t, holdingLiveFixture(t), `INSERT INTO events VALUES (NEW.tenant_id, NEW.seq_hint, NEW.id);
		IF NEW.site_tenant_id IS NOT NULL THEN INSERT INTO events VALUES (NEW.site_tenant_id, NEW.site_seq_hint, NEW.id); END IF;`,
		true, true)
	t.Logf("counter in a separate transaction: %s", o)
	if o.missed == 0 {
		t.Errorf("the separately committed counter missed nothing -- the harness cannot see the class it is for: %s", o)
	}
}

// TestFeedMutant_TwoTriggersLockingCountersDeadlock: the 6th round's two-trigger design -- one
// trigger raises the employer's counter, a second (later in name order) the site's -- so a holding
// tap locks its two counters employer first, and the reverse tap takes the same two in the other
// order: deadlocks (40P01). K-31's one trigger with one canonical lock is the shipped answer.
func TestFeedMutant_TwoTriggersLockingCountersDeadlock(t *testing.T) {
	o := feedLabRun(t, holdingLiveFixture(t), fmt.Sprintf(feedLabRaise, "NEW.tenant_id", "NEW.tenant_id")+` IF NEW.site_tenant_id IS NOT NULL THEN PERFORM pg_sleep(0.002); END IF;`,
		false, true,
		`CREATE FUNCTION %[1]s.feed_site() RETURNS trigger LANGUAGE plpgsql SET search_path = %[1]s, pg_catalog AS $$
			DECLARE s bigint;
			BEGIN IF NEW.site_tenant_id IS NOT NULL THEN `+fmt.Sprintf(feedLabRaise, "NEW.site_tenant_id", "NEW.site_tenant_id")+` END IF; RETURN NULL; END $$`,
		`CREATE TRIGGER b_feed_site AFTER INSERT ON %[1]s.tx FOR EACH ROW EXECUTE FUNCTION %[1]s.feed_site()`)
	t.Logf("two triggers: %s", o)
	if o.deadlocks == 0 {
		t.Errorf("two triggers locking the two counters met no deadlock -- the harness cannot see the class it is for: %s", o)
	}
}

// TestFeedMutant_PreLockWithoutMakeExistDeadlocks: K-31's pre-lock WITHOUT its "make exist" step
// (and so without the count it asserts), on counter rows that do not exist (the 7th round's
// mutant -- 324 deadlocks in 8 640): FOR UPDATE skips a row that is not there, the two upserts then
// take the rows' insertion locks employer first, and the reverse taps deadlock. The lab, raced
// like TestFeed_MissingCounterRowsNoDeadlock (fresh ids each round, 12 holding taps on a barrier).
// CONTROL: the same lab WITH the step and the count -- 0.
func TestFeedMutant_PreLockWithoutMakeExistDeadlocks(t *testing.T) {
	f := holdingLiveFixture(t)
	writers := feedWriters(t, f, 12)
	burst := func(body string) (deadlocks int) {
		l := newFeedLab(t, f, body)
		for round := 0; round < feedMissingRounds; round++ {
			d, other := feedBurst(t, f.ctx, writers, l.design(false), feedLabTenants())
			deadlocks += d
			for _, e := range other {
				t.Errorf("round %d: %v", round, e)
			}
		}
		return deadlocks
	}
	if d := burst(feedLabControl(false, false)); d == 0 {
		t.Errorf("the pre-lock without its make-exist step met no deadlock in %d rounds on missing rows", feedMissingRounds)
	} else {
		t.Logf("pre-lock without make-exist, rows missing: %d deadlocks in %d rounds x 12", d, feedMissingRounds)
	}
	if d := burst(feedLabControl(true, true)); d != 0 {
		t.Errorf("CONTROL: the pre-lock with make-exist, rows missing: %d deadlocks", d)
	}
}
