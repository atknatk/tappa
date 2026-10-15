package apikey_test

// idempotency_db_test.go -- the write runner (Writes.Run, M11 API-4, ADR 0028 §8) against real
// Postgres: one effect for N concurrent requests with one Idempotency-Key, 422 for a different body,
// no stuck key after a death mid-request, and the 24-hour lazy delete scoped to the requesting tenant.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// writeWorld is a business whose API is open, with one key and the runner.
type writeWorld struct {
	d      *db.DB
	trail  *audit.Recorder
	writes *apikey.Writes
	b      business
	p      apikey.Principal
}

func newWriteWorld(t *testing.T, d *db.DB) writeWorld {
	t.Helper()
	b := newBusiness(t, d)
	setAPI(t, b.tenant, true)
	s := newService(t, d, apikey.LivePrefix)
	c, err := s.keys.Create(context.Background(), apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "writes"})
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	w, err := apikey.NewWrites(d)
	if err != nil {
		t.Fatal(err)
	}
	return writeWorld{d: d, trail: trail, writes: w, b: b, p: apikey.Principal{TenantID: b.tenant, KeyID: c.ID}}
}

func reqHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// probe is a write with a committed effect: one audit row whose target is the idempotency key, and
// a fresh record id as its outcome. calls counts every time the work itself ran.
func (w writeWorld) probe(target string, calls *atomic.Int64) apikey.WorkFunc {
	return func(ctx context.Context, tx pgx.Tx) (apikey.Outcome, error) {
		calls.Add(1)
		if _, err := w.trail.RecordTx(ctx, tx, audit.Event{TenantID: w.p.TenantID, Action: "api.idempotency_probe", Target: target}); err != nil {
			return apikey.Outcome{}, err
		}
		ref := uuid.New()
		return apikey.Outcome{Status: 201, Ref: &ref}, nil
	}
}

// ledger reads the business's ledger row for one idempotency key as the owner: (rows, status).
func ledger(t *testing.T, tenant uuid.UUID, idem string) (n int64) {
	t.Helper()
	c := ownerConn(t)
	if err := c.QueryRow(context.Background(), `SELECT count(*) FROM api_idempotency WHERE tenant_id = $1 AND idem_key = $2`, tenant, idem).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ownerConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping (the owner reads the ledger)")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestIdempotency_ConcurrentSameKeyOneEffect: N requests with one Idempotency-Key and one request,
// released together, while the first claim is held open (the runner's afterClaim stop, 300 ms) -- so
// the other N-1 are measured WAITING on the primary key, not merely arriving late. Exactly one runs
// the work and commits its effect (one audit row); the other N-1 replay its outcome -- the same
// status and the same record id -- without running the work. Under -race.
//
// MUTANT (measured, API-4 report): the claim replaced by a read of the ledger before the work and an
// insert after it (read-then-write) -> N works, N effects.
func TestIdempotency_ConcurrentSameKeyOneEffect(t *testing.T) {
	d := appDB(t)
	w := newWriteWorld(t, d)
	idem := "order-" + uuid.NewString()
	var once sync.Once
	apikey.SetAfterClaimForTest(w.writes, func(context.Context, pgx.Tx) {
		once.Do(func() { time.Sleep(300 * time.Millisecond) })
	})
	const n = 16
	var (
		calls  atomic.Int64
		start  = make(chan struct{})
		wg     sync.WaitGroup
		mu     sync.Mutex
		fresh  []apikey.Outcome
		replay []apikey.Outcome
		errs   []error
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out, replayed, err := w.writes.Run(context.Background(), apikey.WriteRequest{
				Principal: w.p, IdempotencyKey: idem, RequestHash: reqHash("PUT /v1/x {}"),
			}, w.probe(idem, &calls))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errs = append(errs, err)
			case replayed:
				replay = append(replay, out)
			default:
				fresh = append(fresh, out)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("%d of %d requests failed: %v", len(errs), n, errs[0])
	}
	if len(fresh) != 1 || len(replay) != n-1 || calls.Load() != 1 {
		t.Fatalf("fresh %d, replayed %d, work ran %d times; want 1, %d, 1", len(fresh), len(replay), calls.Load(), n-1)
	}
	for _, r := range replay {
		if r.Status != 201 || r.Ref == nil || *r.Ref != *fresh[0].Ref {
			t.Fatalf("a replay answered %d %v, want the first request's 201 %v", r.Status, r.Ref, *fresh[0].Ref)
		}
	}
	if got := countAction(auditRows(t, d, w.b.tenant), "api.idempotency_probe", idem); got != 1 {
		t.Fatalf("%d committed effects, want exactly 1", got)
	}
	if got := ledger(t, w.b.tenant, idem); got != 1 {
		t.Fatalf("%d ledger rows for the key, want 1", got)
	}
}

// TestIdempotency_SameKeyDifferentBodyIs422: a key used by one request and then by another (a
// different hash -- method, route or body) is ErrIdempotencyMismatch (the surface's 422), the work
// does not run, nothing is written; the first request's own retry still replays. A failed write
// (the work's error) records nothing: its retry runs the work again.
func TestIdempotency_SameKeyDifferentBodyIs422(t *testing.T) {
	d := appDB(t)
	w := newWriteWorld(t, d)
	idem := "k-" + uuid.NewString()
	var calls atomic.Int64
	run := func(hash string) (apikey.Outcome, bool, error) {
		return w.writes.Run(context.Background(), apikey.WriteRequest{Principal: w.p, IdempotencyKey: idem, RequestHash: hash}, w.probe(idem, &calls))
	}
	first, _, err := run(reqHash("one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(reqHash("two")); !errors.Is(err, apikey.ErrIdempotencyMismatch) {
		t.Fatalf("a different request under the same key: %v, want ErrIdempotencyMismatch", err)
	}
	again, replayed, err := run(reqHash("one"))
	if err != nil || !replayed || *again.Ref != *first.Ref {
		t.Fatalf("CONTROL: the first request's retry: %v replayed=%v ref %v, want a replay of %v", err, replayed, again.Ref, *first.Ref)
	}
	if calls.Load() != 1 {
		t.Fatalf("the work ran %d times, want 1", calls.Load())
	}

	failing := "f-" + uuid.NewString()
	var tries atomic.Int64
	boom := errors.New("the write failed")
	work := func(context.Context, pgx.Tx) (apikey.Outcome, error) { tries.Add(1); return apikey.Outcome{}, boom }
	for range 2 {
		if _, _, err := w.writes.Run(context.Background(), apikey.WriteRequest{Principal: w.p, IdempotencyKey: failing, RequestHash: reqHash("x")}, work); !errors.Is(err, boom) {
			t.Fatalf("a failing write: %v", err)
		}
	}
	if tries.Load() != 2 || ledger(t, w.b.tenant, failing) != 0 {
		t.Fatalf("a failed write: tries %d, ledger rows %d; want 2 and 0 (an error records nothing)", tries.Load(), ledger(t, w.b.tenant, failing))
	}
	for _, bad := range []string{"a b", "ç", string(make([]byte, 256))} {
		if _, _, err := w.writes.Run(context.Background(), apikey.WriteRequest{Principal: w.p, IdempotencyKey: bad, RequestHash: reqHash("x")}, work); !errors.Is(err, apikey.ErrIdempotencyKeyShape) {
			t.Errorf("idempotency key %q: %v, want ErrIdempotencyKeyShape", bad, err)
		}
	}
}

// TestIdempotency_DeathMidRequestLeavesNoStuckKey: a request is stopped after its claim, inside its
// transaction, two ways -- its server process killed (pg_terminate_backend on its connection, the
// process dying as the database sees it) and its context cancelled (the client gone). Either way the
// ledger holds NO row for the key afterwards and the next request with the key runs the work as a
// first request: nothing is stuck "processing" (the ADR's eliminated option).
func TestIdempotency_DeathMidRequestLeavesNoStuckKey(t *testing.T) {
	d := appDB(t)
	w := newWriteWorld(t, d)
	owner := ownerConn(t)
	for _, how := range []string{"terminated", "cancelled"} {
		t.Run(how, func(t *testing.T) {
			idem := how + "-" + uuid.NewString()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			claimed := make(chan int32, 1)
			var calls atomic.Int64
			work := func(ctx context.Context, tx pgx.Tx) (apikey.Outcome, error) {
				calls.Add(1)
				var pid int32
				if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return apikey.Outcome{}, err
				}
				claimed <- pid
				// Hold the transaction open until the request is killed.
				_, err := tx.Exec(ctx, `SELECT pg_sleep(30)`)
				return apikey.Outcome{}, err
			}
			done := make(chan error, 1)
			go func() {
				_, _, err := w.writes.Run(ctx, apikey.WriteRequest{Principal: w.p, IdempotencyKey: idem, RequestHash: reqHash("x")}, work)
				done <- err
			}()
			pid := <-claimed
			if n := ledgerUncommitted(t, owner, w.b.tenant, idem); n != 0 {
				t.Fatalf("CONTROL: the claim is visible to another session before commit (%d rows)", n)
			}
			if how == "terminated" {
				if _, err := owner.Exec(context.Background(), `SELECT pg_terminate_backend($1)`, pid); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("the stopped request reported success")
				}
			case <-time.After(20 * time.Second):
				t.Fatal("the stopped request did not return")
			}
			if n := ledger(t, w.b.tenant, idem); n != 0 {
				t.Fatalf("%d ledger rows after the request died, want 0 (a stuck key)", n)
			}
			var again atomic.Int64
			out, replayed, err := w.writes.Run(context.Background(), apikey.WriteRequest{Principal: w.p, IdempotencyKey: idem, RequestHash: reqHash("x")}, w.probe(idem, &again))
			if err != nil || replayed || again.Load() != 1 || out.Status != 201 {
				t.Fatalf("the next request with the key: err %v replayed %v work %d status %d; want a first request", err, replayed, again.Load(), out.Status)
			}
		})
	}
}

func ledgerUncommitted(t *testing.T, owner *pgx.Conn, tenant uuid.UUID, idem string) (n int64) {
	t.Helper()
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM api_idempotency WHERE tenant_id = $1 AND idem_key = $2`, tenant, idem).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestIdempotency_LazyDeleteIsThisTenantsAlone: rows older than 24 hours are deleted on the request
// path, for the requesting tenant alone (ADR 0028 §8, K-19: no cross-tenant sweep). A's and B's
// ledgers each hold an expired and a fresh row (backdated by the owner); a write of A's deletes A's
// expired row, keeps A's fresh one and leaves BOTH of B's. An expired key is free: a request with it
// -- even a different request -- is a first request, not a replay and not a 422. And the same
// idempotency key B sends never answers with A's result.
func TestIdempotency_LazyDeleteIsThisTenantsAlone(t *testing.T) {
	d := appDB(t)
	a, b := newWriteWorld(t, d), newWriteWorld(t, d)
	owner := ownerConn(t)
	backdate := func(w writeWorld, idem string, age time.Duration) {
		t.Helper()
		if _, err := owner.Exec(context.Background(), `INSERT INTO api_idempotency (tenant_id, key_id, idem_key, request_hash, status, result_ref, created_at)
		                                                VALUES ($1, $2, $3, $4, 201, $5, now() - make_interval(secs => $6))`,
			w.p.TenantID, w.p.KeyID, idem, reqHash("old"), uuid.New(), age.Seconds()); err != nil {
			t.Fatal(err)
		}
	}
	tag := uuid.NewString()
	backdate(a, "a-old-"+tag, 25*time.Hour)
	backdate(a, "a-new-"+tag, 23*time.Hour)
	backdate(a, "a-reuse-"+tag, 25*time.Hour)
	backdate(b, "b-old-"+tag, 25*time.Hour)
	backdate(b, "b-new-"+tag, time.Hour)
	var calls atomic.Int64
	if _, _, err := a.writes.Run(context.Background(), apikey.WriteRequest{Principal: a.p}, a.probe("plain", &calls)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tenant uuid.UUID
		idem   string
		want   int64
	}{
		{a.p.TenantID, "a-old-" + tag, 0},
		{a.p.TenantID, "a-new-" + tag, 1},
		{b.p.TenantID, "b-old-" + tag, 1},
		{b.p.TenantID, "b-new-" + tag, 1},
	} {
		if got := ledger(t, c.tenant, c.idem); got != c.want {
			t.Errorf("%s: %d rows after A's write, want %d", c.idem, got, c.want)
		}
	}
	out, replayed, err := a.writes.Run(context.Background(), apikey.WriteRequest{Principal: a.p, IdempotencyKey: "a-reuse-" + tag, RequestHash: reqHash("new")}, a.probe("reuse", &calls))
	if err != nil || replayed || out.Status != 201 {
		t.Fatalf("an expired key reused by a different request: err %v replayed %v; want a first request", err, replayed)
	}
	// B sends the key A recorded: its own first request, never A's result.
	shared := "a-new-" + tag
	var bCalls atomic.Int64
	if _, replayed, err := b.writes.Run(context.Background(), apikey.WriteRequest{Principal: b.p, IdempotencyKey: shared, RequestHash: reqHash("old")}, b.probe(shared, &bCalls)); err != nil || replayed || bCalls.Load() != 1 {
		t.Fatalf("B with A's idempotency key: err %v replayed %v work %d; want B's own first request", err, replayed, bCalls.Load())
	}
}

type whoKey struct{}

// TestIdempotency_TwoKeysOverAFullSweepNeitherDeadlockNorWait (API-4 review D1): a tenant with more
// than 100 expired ledger rows, and two writes with DIFFERENT keys whose own expired rows sit in each
// other's sweep -- B's key's row the oldest, A's at rank 151. A sweeps first, then B; both are then
// released together, and A holds its transaction open for a second after its claim. Neither write
// fails (no 40P01) and B does not wait for A: it returns well inside A's second. Second part: the
// key's own expired row is deleted even when the sweep does not reach it (150 older rows in front of
// it) -- the same key with another request is a first request, not 422.
//
// MUTANTS (measured, API-4 review report): the sweep moved back into the write's transaction ->
// 40P01; DeleteExpiredIdempotencyForKey removed -> 422 in the second part.
func TestIdempotency_TwoKeysOverAFullSweepNeitherDeadlockNorWait(t *testing.T) {
	d := appDB(t)
	owner := ownerConn(t)
	expired := func(w writeWorld, idem string, age string) {
		t.Helper()
		if _, err := owner.Exec(context.Background(), `INSERT INTO api_idempotency (tenant_id, key_id, idem_key, request_hash, status, created_at)
		                                                VALUES ($1, $2, $3, $4, 201, now() - $5::interval)`,
			w.p.TenantID, w.p.KeyID, idem, reqHash("old"), age); err != nil {
			t.Fatal(err)
		}
	}
	fillers := func(w writeWorld, n int) {
		t.Helper()
		if _, err := owner.Exec(context.Background(), `INSERT INTO api_idempotency (tenant_id, key_id, idem_key, request_hash, status, created_at)
		                                                SELECT $1, $2, 'f-' || i, $3, 201, now() - interval '48 hours' - i * interval '1 minute'
		                                                  FROM generate_series(1, $4::int) AS i`,
			w.p.TenantID, w.p.KeyID, reqHash("old"), n); err != nil {
			t.Fatal(err)
		}
	}

	w := newWriteWorld(t, d)
	fillers(w, 249)
	ka, kb := "ka-"+uuid.NewString(), "kb-"+uuid.NewString()
	expired(w, kb, "100 hours")                       // rank 1: A's sweep takes it
	expired(w, ka, "48 hours 100 minutes 30 seconds") // rank 151: B's sweep takes it
	aSwept, bSwept, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	apikey.SetBeforeKeyForTest(w.writes, func(ctx context.Context) {
		switch ctx.Value(whoKey{}) {
		case "A":
			close(aSwept)
		case "B":
			close(bSwept)
		}
		<-release
	})
	apikey.SetAfterClaimForTest(w.writes, func(ctx context.Context, _ pgx.Tx) {
		if ctx.Value(whoKey{}) == "A" {
			time.Sleep(time.Second)
		}
	})
	var calls atomic.Int64
	type result struct {
		err  error
		took time.Duration
	}
	run := func(who, idem string, out chan<- result) {
		ctx := context.WithValue(context.Background(), whoKey{}, who)
		_, _, err := w.writes.Run(ctx, apikey.WriteRequest{Principal: w.p, IdempotencyKey: idem, RequestHash: reqHash("new")}, w.probe(idem, &calls))
		out <- result{err: err}
	}
	aDone, bDone := make(chan result, 1), make(chan result, 1)
	go run("A", ka, aDone)
	<-aSwept
	go run("B", kb, bDone)
	<-bSwept
	start := time.Now()
	close(release)
	b := <-bDone
	b.took = time.Since(start)
	a := <-aDone
	if a.err != nil || b.err != nil {
		t.Fatalf("A: %v; B: %v -- want both to succeed (no deadlock)", a.err, b.err)
	}
	if b.took > 600*time.Millisecond {
		t.Fatalf("B took %v while A held its transaction open for 1s: a write waited on another key's", b.took)
	}
	t.Logf("B returned %v after the release, A held its transaction 1s", b.took.Round(time.Millisecond))

	// Second part: the key's own row behind 150 older ones.
	w2 := newWriteWorld(t, d)
	fillers(w2, 150)
	kc := "kc-" + uuid.NewString()
	expired(w2, kc, "25 hours")
	var c2 atomic.Int64
	if _, replayed, err := w2.writes.Run(context.Background(), apikey.WriteRequest{Principal: w2.p, IdempotencyKey: kc, RequestHash: reqHash("another")}, w2.probe(kc, &c2)); err != nil || replayed || c2.Load() != 1 {
		t.Fatalf("an expired key behind 150 older rows, reused: err %v replayed %v work %d; want a first request", err, replayed, c2.Load())
	}
}
