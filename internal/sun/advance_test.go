package sun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Fast unit tests: the wrapper's error mapping and argument pass-through, proven
// with a fake counterAdvancer so they run WITHOUT a database. The atomicity
// itself lives in the SQL and is proven against real Postgres below; these
// tests only pin the thin Go glue (0-row -> ErrReplay, other error -> wrapped,
// ctr -> int32).
// ---------------------------------------------------------------------------

// fakeAdvancer is a counterAdvancer test double: it returns a fixed row/error
// and records the argument it was called with. It implements ONLY the one
// method AdvanceCounter needs -- proof that the consumer-side interface keeps
// the wrapper testable without the whole store.Querier surface.
type fakeAdvancer struct {
	row   store.AdvanceTagCounterRow
	err   error
	got   store.AdvanceTagCounterParams
	calls int
}

func (f *fakeAdvancer) AdvanceTagCounter(_ context.Context, arg store.AdvanceTagCounterParams) (store.AdvanceTagCounterRow, error) {
	f.calls++
	f.got = arg
	return f.row, f.err
}

func TestAdvanceCounter_SuccessReturnsGap(t *testing.T) {
	f := &fakeAdvancer{row: store.AdvanceTagCounterRow{Uid: "aabbccddeeff00", CtrGap: 4}}
	gap, err := AdvanceCounter(context.Background(), f, uuid.New(), "aabbccddeeff00", 5)
	if err != nil {
		t.Fatalf("AdvanceCounter success err = %v, want nil", err)
	}
	if gap != 4 {
		t.Fatalf("gap = %d, want 4 (returned verbatim from the query row)", gap)
	}
}

func TestAdvanceCounter_NoRowsMapsToErrReplay(t *testing.T) {
	f := &fakeAdvancer{err: pgx.ErrNoRows}
	gap, err := AdvanceCounter(context.Background(), f, uuid.New(), "aabbccddeeff00", 5)
	if !errors.Is(err, ErrReplay) {
		t.Fatalf("err = %v, want ErrReplay (0 rows -> replay/race reject)", err)
	}
	if gap != 0 {
		t.Fatalf("gap = %d, want 0 on reject", gap)
	}
}

func TestAdvanceCounter_OtherErrorIsWrappedNotReplay(t *testing.T) {
	underlying := errors.New("connection reset by peer")
	f := &fakeAdvancer{err: underlying}
	_, err := AdvanceCounter(context.Background(), f, uuid.New(), "aabbccddeeff00", 5)
	if errors.Is(err, ErrReplay) {
		t.Fatal("a generic DB failure must NOT be reported as ErrReplay (reject != outage)")
	}
	if !errors.Is(err, underlying) {
		t.Fatalf("err = %v, want it to wrap the underlying failure", err)
	}
}

// TestAdvanceCounter_PassesArgsThrough confirms tenant, uid and the ctr->int32
// conversion reach the query unchanged, including the largest valid 24-bit
// counter (2^24-1), which must stay positive in int32.
func TestAdvanceCounter_PassesArgsThrough(t *testing.T) {
	const maxCtr uint32 = 1<<24 - 1 // 16_777_215, the wrap point of the 24-bit ctr
	f := &fakeAdvancer{row: store.AdvanceTagCounterRow{CtrGap: 0}}
	tid := uuid.New()
	uid := "0123456789abcd"

	if _, err := AdvanceCounter(context.Background(), f, tid, uid, maxCtr); err != nil {
		t.Fatalf("AdvanceCounter err = %v, want nil", err)
	}
	if f.calls != 1 {
		t.Fatalf("AdvanceTagCounter called %d times, want exactly 1", f.calls)
	}
	if f.got.TenantID != tid {
		t.Errorf("tenant = %s, want %s", f.got.TenantID, tid)
	}
	if f.got.Uid != uid {
		t.Errorf("uid = %q, want %q", f.got.Uid, uid)
	}
	if f.got.Ctr != int32(maxCtr) || f.got.Ctr <= 0 {
		t.Errorf("ctr = %d, want %d (lossless, positive int32)", f.got.Ctr, int32(maxCtr))
	}
}

// ---------------------------------------------------------------------------
// Real-Postgres proof (CLAUDE.md §8, Q04): the atomic replay guard cannot be
// proven against a fake DB. These tests connect as tappa_app via DATABASE_URL
// (RLS in force) and skip when it is unset.
// ---------------------------------------------------------------------------

const (
	raceGoroutines = 50 // N concurrent taps per race round
	raceRounds     = 5  // repeat the race so a single `go test` surfaces flakiness
)

// appDB returns an UNPINNED tappa_app pool sized to hold the whole race at once.
// Without pool_max_conns the goroutines would queue on pgx's small default pool
// and the "race" would degrade into a near-sequential run -- the atomicity proof
// depends on many transactions genuinely contending on the same row lock, so we
// widen the pool to raceGoroutines + headroom (internal/db.New reads the DSN, and
// pgx honours pool_max_conns as a query parameter).
func appDB(t *testing.T) *db.DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping SUN counter DB tests (real Postgres required, Q04)")
	}
	if !strings.Contains(raw, "pool_max_conns") {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		raw += sep + "pool_max_conns=" + strconv.Itoa(raceGoroutines+4)
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// randUID returns a fresh 14-hex-char tag uid (7 bytes) in the CANONICAL UPPER
// case migration 00013's tags_uid_canonical_hex requires -- the same spelling
// Parse produces from a URL (params.go). Random so repeated runs never collide
// (fixtures are intentionally not cleaned up -- tappa_app cannot DELETE tags,
// redline R5), which is exactly why the case matters here: every run leaves rows
// behind, and before 00013 this helper's lower-case output was accepted as a
// SEPARATE primary key carrying the SAME wrapped-key AAD (backlog T8).
func randUID(t *testing.T) string {
	t.Helper()
	var b [7]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// newTagFixture commits a fresh tenant + location + one active tag with the given
// starting last_ctr, all as tappa_app inside its own tenant context, and returns
// what the counter test needs to address that tag.
func newTagFixture(t *testing.T, d *db.DB, startCtr int32) (tenantID uuid.UUID, uid string) {
	t.Helper()
	tenantID = uuid.New()
	locationID := uuid.New()
	uid = randUID(t)

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'sun-advance-test', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'loc', 35.899, 14.514)`,
			locationID, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, '\xDEAD', $4, 'active')`,
			uid, tenantID, locationID, startCtr)
		return e
	})
	if err != nil {
		t.Fatalf("newTagFixture: %v", err)
	}
	return tenantID, uid
}

// advance runs sun.AdvanceCounter inside a fresh tenant-scoped transaction (its
// own pooled connection), exactly as M2-07's Verify will. It returns the gap and
// the error verbatim -- WithTenant passes fn's error through unwrapped, so an
// ErrReplay from AdvanceCounter stays errors.Is-matchable at the call site.
func advance(t *testing.T, d *db.DB, ctx context.Context, tenantID uuid.UUID, uid string, ctr uint32) (int32, error) {
	t.Helper()
	var gap int32
	err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		gap, e = AdvanceCounter(ctx, store.New(tx), tenantID, uid, ctr)
		return e
	})
	return gap, err
}

// finalCtr reads the tag's settled last_ctr context-less via the resolver.
func finalCtr(t *testing.T, d *db.DB, uid string) int32 {
	t.Helper()
	got, err := d.GetTagByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("read final tag: %v", err)
	}
	return got.LastCtr
}

// TestAdvanceCounter_ReplayAndBackwardRejected: a replayed (equal) ctr and a
// backward (smaller) ctr are both rejected with ErrReplay via the strict
// less-than guard, and neither moves last_ctr. This is the strict (not
// greater-or-equal) comparison proven end to end: >= would let the equal ctr through.
func TestAdvanceCounter_ReplayAndBackwardRejected(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, uid := newTagFixture(t, d, 10)

	// Equal ctr (replay) -> reject, last_ctr stays 10.
	if _, err := advance(t, d, ctx, tenantID, uid, 10); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay ctr=10 err = %v, want ErrReplay", err)
	}
	// Smaller ctr (backward counter) -> reject, last_ctr stays 10.
	if _, err := advance(t, d, ctx, tenantID, uid, 9); !errors.Is(err, ErrReplay) {
		t.Fatalf("backward ctr=9 err = %v, want ErrReplay", err)
	}
	if got := finalCtr(t, d, uid); got != 10 {
		t.Fatalf("last_ctr = %d after two rejected advances, want 10 (unchanged)", got)
	}
}

// TestAdvanceCounter_ForwardJumpAcceptedWithGap: a ctr far beyond last_ctr is
// accepted (strict '>' is enough, we do NOT require last_ctr+1) and the returned
// gap is ctr - last_ctr - 1. No threshold is applied here -- the gap is data for
// the base:ctr-gap-review policy (M3-06), not a reject.
func TestAdvanceCounter_ForwardJumpAcceptedWithGap(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, uid := newTagFixture(t, d, 100)

	gap, err := advance(t, d, ctx, tenantID, uid, 100+5000)
	if err != nil {
		t.Fatalf("forward jump ctr=5100 err = %v, want accept", err)
	}
	if gap != 4999 { // 5100 - 100 - 1
		t.Fatalf("gap = %d, want 4999 (ctr - last_ctr - 1)", gap)
	}
	if got := finalCtr(t, d, uid); got != 5100 {
		t.Fatalf("last_ctr = %d, want 5100", got)
	}
}

// TestAdvanceCounter_ConsecutiveGapZero: the normal case -- ctr = last_ctr + 1
// advances with gap 0 (no reads skipped).
func TestAdvanceCounter_ConsecutiveGapZero(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, uid := newTagFixture(t, d, 0)

	gap, err := advance(t, d, ctx, tenantID, uid, 1)
	if err != nil {
		t.Fatalf("ctr=1 err = %v, want accept", err)
	}
	if gap != 0 {
		t.Fatalf("gap = %d, want 0 (consecutive counters skip nothing)", gap)
	}
}

// TestAdvanceCounter_ConcurrentRaceExactlyOneWinner is THE proof of §4.4: the
// replay guard is atomic in the DB, not in Go. N goroutines, each on its OWN
// pooled connection / transaction, race to advance the SAME (uid, ctr). Exactly
// one must win; the other N-1 must see ErrReplay (0 rows). There is deliberately
// NO sync.Mutex around the advance -- that would be a single-process fiction and
// would also make this test pass for the wrong reason (serialised, not atomic).
// A start barrier (launched.Wait then close(start)) releases every goroutine at
// once so they genuinely contend on the row lock; the only shared state is the
// atomic tally, touched via sync/atomic so -race stays clean. Repeated over
// raceRounds fresh tags so a single run surfaces any flake.
func TestAdvanceCounter_ConcurrentRaceExactlyOneWinner(t *testing.T) {
	d := appDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for round := 0; round < raceRounds; round++ {
		const raceCtr uint32 = 7
		tenantID, uid := newTagFixture(t, d, 0)

		var wins, replays, others int64
		var launched sync.WaitGroup // every goroutine parked and ready
		var done sync.WaitGroup     // every goroutine finished
		launched.Add(raceGoroutines)
		done.Add(raceGoroutines)
		start := make(chan struct{}) // closed to release all at once

		for i := 0; i < raceGoroutines; i++ {
			go func() {
				defer done.Done()
				launched.Done()
				<-start // block until every goroutine is ready, then rush together

				_, err := advance(t, d, ctx, tenantID, uid, raceCtr)
				switch {
				case err == nil:
					atomic.AddInt64(&wins, 1)
				case errors.Is(err, ErrReplay):
					atomic.AddInt64(&replays, 1)
				default:
					atomic.AddInt64(&others, 1)
					t.Errorf("round %d: unexpected error (want nil or ErrReplay): %v", round, err)
				}
			}()
		}

		launched.Wait() // all goroutines are parked on <-start: now the race is real
		close(start)
		done.Wait()

		if wins != 1 {
			t.Fatalf("round %d: winners = %d, want EXACTLY 1 (atomic replay guard)", round, wins)
		}
		if replays != raceGoroutines-1 {
			t.Fatalf("round %d: ErrReplay = %d, want %d (all losers rejected)", round, replays, raceGoroutines-1)
		}
		if others != 0 {
			t.Fatalf("round %d: %d goroutines got a non-replay error", round, others)
		}
		if got := finalCtr(t, d, uid); got != int32(raceCtr) {
			t.Fatalf("round %d: final last_ctr = %d, want %d (winner's ctr)", round, got, raceCtr)
		}
	}
}
