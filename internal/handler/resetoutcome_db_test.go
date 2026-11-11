package handler

// resetoutcome_db_test.go -- T197 against real Postgres: a recovery link's withdrawal and its
// outcome row, each on its own clock, tried once more on a transient failure, with a stalled
// database produced by row locks taken in another session and by a TCP proxy that goes down or loses
// an answer. Row locks stay on this test's own rows, so nothing else in the database waits.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// outcomeWorld is n businesses with one active administrator each behind ONE address, the real
// adminauth.Resets, the real audit recorder and a logger over a buffer: a request mints n real links.
type outcomeWorld struct {
	data    *db.DB
	owner   *pgx.Conn
	tenants []uuid.UUID
	email   string
	emails  []string
	real    *adminauth.Resets
	flow    *AdminReset
	router  *chi.Mux
	logged  *outcomeLog
	mu      sync.Mutex
	minted  []adminauth.ResetGrant
	// afterMint runs between the mint and the fallback writes, on the request's goroutine.
	afterMint func([]adminauth.ResetGrant)
}

type outcomeWorldOptions struct {
	n int
	// addresses is how many addresses each business has one administrator for (default 1), so
	// that many requests can each mint n links.
	addresses int
	channel   ResetChannel
	proxy     *faultProxy
}

func newOutcomeWorld(t *testing.T, o outcomeWorldOptions) *outcomeWorld {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are required (real Postgres; the stalling locks are taken as the owner)")
	}
	ctx := context.Background()
	if o.proxy != nil {
		dsn = o.proxy.dsn(t, dsn)
	}
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(withSmallPool(dsn))})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := owner.Close(context.Background()); err != nil {
			t.Errorf("close the owner connection: %v", err)
		}
	})
	w := &outcomeWorld{data: data, owner: owner, logged: &outcomeLog{}}
	if o.addresses == 0 {
		o.addresses = 1
	}
	for i := 0; i < o.addresses; i++ {
		w.emails = append(w.emails, "t197-"+uuid.NewString()+"@outcome.example")
	}
	w.email = w.emails[0]
	release := holdPanelTablesLock(t)
	for i := 0; i < o.n; i++ {
		id := uuid.New()
		w.tenants = append(w.tenants, id)
		if err := data.WithTenant(ctx, id, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
				VALUES ($1, 'Outcome Ltd', $2, 'bar', 'single')`, id, "VAT-"+id.String()); e != nil {
				return e
			}
			for _, email := range w.emails {
				if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
					VALUES ($1, $2, 'Outcome Owner', $3, $4, 'owner', 'active')`, uuid.New(), id, email, ownerDigest); e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("insert the business: %v", err)
		}
	}
	release()
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	real, err := adminauth.NewResets(data, adminTestConfig())
	if err != nil {
		t.Fatalf("adminauth.NewResets: %v", err)
	}
	w.real = real
	w.flow, err = NewAdminReset(&mintHook{Resets: real, w: w}, o.channel, trail, adminTestConfig(),
		slog.New(slog.NewTextHandler(w.logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := w.flow.Drain(ctx); err != nil {
			t.Errorf("the outbox did not drain at cleanup: %v", err)
		}
	})
	w.router = chi.NewRouter()
	w.flow.Mount(w.router)
	return w
}

// mintHook is the real Resets with a pause between the mint and everything after it: the only moment
// a lock on a row this very request creates can be taken.
type mintHook struct {
	*adminauth.Resets
	w *outcomeWorld
}

func (m *mintHook) IssueForEmail(ctx context.Context, email string) ([]adminauth.ResetGrant, []adminauth.Reset, []adminauth.StalledReset, error) {
	g, k, st, err := m.Resets.IssueForEmail(ctx, email)
	if err == nil && len(g) > 0 {
		m.w.mu.Lock()
		m.w.minted = append(m.w.minted, g...)
		m.w.mu.Unlock()
		if m.w.afterMint != nil {
			m.w.afterMint(g)
		}
	}
	return g, k, st, err
}

// mintedCount is how many links the requests minted (mintHook).
func (w *outcomeWorld) mintedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.minted)
}

// outcomeLog is a buffer two goroutines can log into, counted by sentence.
type outcomeLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *outcomeLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *outcomeLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *outcomeLog) count(sub string) int { return strings.Count(l.String(), sub) }

// stall starts a transaction on the owner's connection holding FOR UPDATE on what blocks the chosen
// write: a link's row blocks its withdrawal's UPDATE, a tenant's row blocks audit_log's foreign-key
// check (00005). It returns the release.
func (w *outcomeWorld) stall(t *testing.T, links, rows bool) (release func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := w.owner.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the stalling transaction: %v", err)
	}
	if links {
		w.mu.Lock()
		ids := make([]uuid.UUID, 0, len(w.minted))
		for _, g := range w.minted {
			ids = append(ids, g.Issued.Reset.ID)
		}
		w.mu.Unlock()
		if _, err := tx.Exec(ctx, `SELECT 1 FROM password_resets WHERE id = ANY($1) FOR UPDATE`, ids); err != nil {
			t.Fatalf("lock the links: %v", err)
		}
	}
	if rows {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM tenants WHERE id = ANY($1) FOR UPDATE`, w.tenants); err != nil {
			t.Fatalf("lock the businesses: %v", err)
		}
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if err := tx.Rollback(context.Background()); err != nil {
				t.Errorf("release the stalling locks: %v", err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

func (w *outcomeWorld) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.owner.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE tenant_id = ANY($1) AND action = $2`, w.tenants, ActionAdminResetUndelivered).Scan(&n); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	return n
}

func (w *outcomeWorld) liveLinks(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.owner.QueryRow(context.Background(),
		`SELECT count(*) FROM password_resets WHERE tenant_id = ANY($1) AND cancelled_at IS NULL AND used_at IS NULL`, w.tenants).Scan(&n); err != nil {
		t.Fatalf("count the live links: %v", err)
	}
	return n
}

func (w *outcomeWorld) request(t *testing.T) {
	t.Helper()
	requestReset(t, newBrowser(t, w.router), w.email)
}

func (w *outcomeWorld) finish(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := w.flow.Drain(ctx); err != nil {
		t.Fatalf("the outbox did not drain: %v", err)
	}
}

// The two ERROR lines of a write that is lost, and the WARN of one that is tried again.
const (
	lineWithdrawalLost = "withdrawing an undelivered link failed"
	lineIdentityLost   = "withdrawing an undelivered identity link failed"
	lineRowLost        = "audit write failed"
	lineWillRetry      = "is tried once more"
)

// TestResetOutcomeDB_AStalledWithdrawalLeavesTheRowItsOwnClock is the fallback half of T197's finding,
// with the shipped clocks: the link's row is locked, so the withdrawal cannot finish, and the outcome
// row, which that lock does not touch, is there WHEN THE RESPONSE RETURNS. Then the lock goes and the
// worker's second try withdraws the link. Before T197 the same setup gave 0 rows and a live link.
//
// MUTATION: the row on the withdrawal's context -> the row is not there at the response -> red.
func TestResetOutcomeDB_AStalledWithdrawalLeavesTheRowItsOwnClock(t *testing.T) {
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: refusingChannel{&recordingChannel{}}})
	var release func()
	w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, true, false) }

	w.request(t)
	if n := w.rows(t); n != 1 {
		t.Errorf("%d row(s) when the response returned, want 1: the withdrawal's stall took the row's time", n)
	}
	if n := w.liveLinks(t); n != 1 {
		t.Fatalf("CONTROL: %d live link(s) while the lock is held, want 1: the withdrawal was not stalled", n)
	}
	release()
	w.finish(t)
	if n := w.liveLinks(t); n != 0 {
		t.Errorf("%d live link(s) after the second try, want 0: the owner cannot ask again for ResetTTL", n)
	}
	if n := w.rows(t); n != 1 {
		t.Errorf("%d row(s) at the end, want exactly 1", n)
	}
	if n := w.logged.count(lineWithdrawalLost) + w.logged.count(lineRowLost); n != 0 {
		t.Errorf("%d lost-write line(s), want none:\n%s", n, w.logged)
	}
}

// TestResetOutcomeDB_AStalledRowIsWrittenOnTheSecondTry is the other write: the audit INSERT is what
// waits, the link is withdrawn by the time the response returns, and the worker writes the one row.
//
// MUTATION: no second try (resetOutcomeTries 1) -> the row is lost -> red.
func TestResetOutcomeDB_AStalledRowIsWrittenOnTheSecondTry(t *testing.T) {
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: refusingChannel{&recordingChannel{}}})
	var release func()
	w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, false, true) }

	w.request(t)
	if n := w.liveLinks(t); n != 0 {
		t.Errorf("%d live link(s) when the response returned, want 0: only the row was stalled", n)
	}
	if n := w.rows(t); n != 0 {
		t.Fatalf("CONTROL: %d row(s) while the lock is held, want 0: the row was not stalled", n)
	}
	release()
	w.finish(t)
	if n := w.rows(t); n != 1 {
		t.Errorf("%d row(s) after the second try, want exactly 1", n)
	}
}

// TestResetOutcomeDB_AFullWindowStalledAnswersInsideTheBudget is what the timing has to stay: a full
// adminauth.ResetWindow, both writes stalled, the shipped clocks. The response leaves about one
// resetFallbackBudget after the mint (before T197, with only the withdrawals stalled: 40.5 s for the
// whole request, 0 rows, 8 live links), and no write is lost for it: the worker finishes all eight once
// the database answers.
//
// The bound is on the time from the mint to the response and is 3 x the budget, far under what a
// clock per grant costs (8 s), because the test is about the shape, and the shared machine's load is
// not its subject. MUTATION: the budget per grant, or the second try on the request -> over it -> red.
func TestResetOutcomeDB_AFullWindowStalledAnswersInsideTheBudget(t *testing.T) {
	w := newOutcomeWorld(t, outcomeWorldOptions{n: adminauth.ResetWindow, channel: refusingChannel{&recordingChannel{}}})
	var release func()
	var mintedAt time.Time
	w.afterMint = func([]adminauth.ResetGrant) {
		release = w.stall(t, true, true)
		mintedAt = time.Now()
	}

	w.request(t)
	took := time.Since(mintedAt)
	t.Logf("a full window, every write stalled: answered %v after the mint (budget %v)", took, resetFallbackBudget)
	if ceiling := 3 * resetFallbackBudget; took > ceiling {
		t.Errorf("the response took %v after the mint, over %v: the stall is readable from the clock", took, ceiling)
	}
	release()
	w.finish(t)
	if n := w.rows(t); n != adminauth.ResetWindow {
		t.Errorf("%d row(s), want %d: the budget cost a record (§4.6)", n, adminauth.ResetWindow)
	}
	if n := w.liveLinks(t); n != 0 {
		t.Errorf("%d live link(s), want 0", n)
	}
	if n := w.logged.count(lineWithdrawalLost) + w.logged.count(lineRowLost); n != 0 {
		t.Errorf("%d lost-write line(s), want none:\n%s", n, w.logged)
	}
}

// TestResetOutcomeDB_TheWorkerGivesEachWriteItsOwnClock is recordWorkerOutcome's half: a failed send,
// the link's row locked. The row appears while the lock is held, and the lock is released only after
// that, so a row sharing the withdrawal's clock can never appear.
//
// MUTATION: the worker's row on the withdrawal's context -> no row while the lock is held -> red.
func TestResetOutcomeDB_TheWorkerGivesEachWriteItsOwnClock(t *testing.T) {
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: &recordingChannel{err: errSendFailed}})
	w.flow.clocks.grace = 300 * time.Millisecond
	var release func()
	w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, true, false) }

	w.request(t)
	waitUntil(t, 20*time.Second, "the row while the withdrawal is stalled", func() bool { return w.rows(t) == 1 })
	release()
	w.finish(t)
	if n := w.rows(t); n != 1 {
		t.Errorf("%d row(s), want exactly 1", n)
	}
}

// TestResetOutcomeDB_BothTriesFailingStillFailClosed is the outcome that remains, on both paths: the
// lock never goes while the writes try. Each lost write says so at ERROR, with ids and no address, no
// row exists, the link stays live, and the owner's next request mints nothing -- the availability
// cost EM-7C's limit 8 counts, now only until the list of withdrawals owed makes it (T212; the later
// try is held off here, resetrecovery_db_test.go measures it). It also pins that a lost outcome costs
// two tries of each write and not more.
func TestResetOutcomeDB_BothTriesFailingStillFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel ResetChannel
	}{
		{"the request's fallback", refusingChannel{&recordingChannel{}}},
		{"the worker's outcome", &recordingChannel{err: errSendFailed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: tc.channel})
			w.flow.clocks = resetOutcomeClocks{fallbackBudget: 200 * time.Millisecond, fallbackStep: 100 * time.Millisecond, grace: 100 * time.Millisecond, retryPause: 50 * time.Millisecond,
				// The state before the list's later try (T212, resetrecovery_db_test.go).
				recoveryFirst: time.Hour, recoveryMaxPause: time.Hour, recoveryPass: time.Second}
			var release func()
			w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, true, true) }

			w.request(t)
			waitUntil(t, 20*time.Second, "both lost-write lines", func() bool {
				return w.logged.count(lineWithdrawalLost) == 1 && w.logged.count(lineRowLost) == 1
			})
			release()
			if n := w.rows(t); n != 0 {
				t.Errorf("%d row(s), want 0", n)
			}
			if n := w.liveLinks(t); n != 1 {
				t.Errorf("%d live link(s), want 1: this is the case the limit counts", n)
			}
			if n := w.logged.count(lineWillRetry); n != 2 {
				t.Errorf("%d retry line(s), want one per write:\n%s", n, w.logged)
			}
			if strings.Contains(w.logged.String(), w.email) {
				t.Error("a line carries the address")
			}
			w.minted = nil
			w.afterMint = nil
			w.request(t)
			if w.minted != nil {
				t.Error("a new link while the undelivered one lives: the one-live-link rule moved")
			}
		})
	}
}

// TestResetOutcomeDB_ADroppedConnectionIsTriedOnceMore is the other way a database goes away: not slow
// but gone, every connection cut and refused. The first tries fail at once; the proxy comes back on
// the row's retry line, before the worker's pause has even begun; both writes then succeed.
//
// MUTATION: the dropped connection classified permanent -> no second try -> red.
func TestResetOutcomeDB_ADroppedConnectionIsTriedOnceMore(t *testing.T) {
	proxy := newFaultProxy(t)
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: refusingChannel{&recordingChannel{}}, proxy: proxy})
	w.afterMint = func([]adminauth.ResetGrant) { proxy.down() }
	w.flow.log = slog.New(hookedHandler{
		Handler: slog.NewTextHandler(w.logged, &slog.HandlerOptions{Level: slog.LevelDebug}),
		on: func(r slog.Record) {
			if strings.Contains(r.Message, lineWillRetry) && recordHas(r, "write", "row") {
				proxy.up()
			}
		},
	})

	w.request(t)
	w.finish(t)
	if n := w.rows(t); n != 1 {
		t.Errorf("%d row(s), want exactly 1:\n%s", n, w.logged)
	}
	if n := w.liveLinks(t); n != 0 {
		t.Errorf("%d live link(s), want 0:\n%s", n, w.logged)
	}
	if n := w.logged.count(lineWillRetry); n != 2 {
		t.Errorf("%d retry line(s), want 2, or the proxy never took the writes down:\n%s", n, w.logged)
	}
}

// TestResetOutcomeDB_ALostAnswerDoesNotDoubleAWrite is why the second try is safe. The proxy forwards
// a write's COMMIT, reads the server's answer, and cuts the connection instead of relaying it: the
// write HAPPENED and the caller sees an error, the one case where a retry could double. Measured for
// both writes: one row, one withdrawal, a success on the second try.
//
// MUTATION: the outcome row without its own id (audit.Event.ID) -> two rows -> red.
func TestResetOutcomeDB_ALostAnswerDoesNotDoubleAWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		skip int // COMMITs let through first: the withdrawal's comes before the row's
	}{
		{"the withdrawal's commit", 0},
		{"the row's commit", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := newFaultProxy(t)
			w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: refusingChannel{&recordingChannel{}}, proxy: proxy})
			w.flow.clocks.retryPause = 100 * time.Millisecond
			w.afterMint = func([]adminauth.ResetGrant) { proxy.loseCommitAnswer(tc.skip) }

			w.request(t)
			w.finish(t)
			if !proxy.lostAnswer() {
				t.Fatal("CONTROL: no answer was lost, so nothing was retried")
			}
			if n := w.rows(t); n != 1 {
				t.Errorf("%d row(s), want exactly 1: audit_log cannot take one back", n)
			}
			if n := w.liveLinks(t); n != 0 {
				t.Errorf("%d live link(s), want 0", n)
			}
			if n := w.logged.count(lineWillRetry); n != 1 {
				t.Errorf("%d retry line(s), want the one write's:\n%s", n, w.logged)
			}
			if n := w.logged.count(lineWithdrawalLost) + w.logged.count(lineRowLost); n != 0 {
				t.Errorf("%d lost-write line(s), want none:\n%s", n, w.logged)
			}
		})
	}
}

// awaitMint waits for request i to have minted (its afterMint) before the next one starts. The requests
// reach the stall together either way -- each waits in afterMint -- but they MINT one at a time: fired
// at once, sixteen full windows wait for this world's four connections, and since T236 (a) a mint that
// waits for a connection longer than adminauth.ResetMintLockWait stalls instead of minting (backlog
// T309), so a request with nothing minted would never arrive.
func awaitMint(t *testing.T, arrived <-chan struct{}, i int) {
	t.Helper()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatalf("request %d minted nothing in 30 s", i)
	}
}

// TestResetOutcomeDB_AShortStallDoesNotOverflowTheRetryQueue is the regression an audit measured at a
// queue of 32: a short stall right after the mint, 16 requests of a full window each. Every budget
// runs out and sends its writes to the queue untried. All of them fit: not one write is lost, and once
// the database answers there are 128 rows and no live link.
//
// MUTATION: resetRetryQueueSize 32 -> 33 rows, 95 live links -> red.
func TestResetOutcomeDB_AShortStallDoesNotOverflowTheRetryQueue(t *testing.T) {
	const requests = 16
	w := newOutcomeWorld(t, outcomeWorldOptions{n: adminauth.ResetWindow, addresses: requests, channel: refusingChannel{&recordingChannel{}}})
	arrived, stalled := make(chan struct{}), make(chan struct{})
	w.afterMint = func([]adminauth.ResetGrant) {
		arrived <- struct{}{}
		<-stalled
	}
	type form struct {
		b    *browser
		csrf string
	}
	forms := make([]form, requests)
	for i := range forms {
		b := newBrowser(t, w.router)
		forms[i] = form{b, csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))}
	}
	var answered sync.WaitGroup
	took := make([]time.Duration, requests)
	var start time.Time
	begin := make(chan struct{})
	for i := range forms {
		answered.Add(1)
		go func(i int) {
			defer answered.Done()
			rec := forms[i].b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {forms[i].csrf}, "email": {w.emails[i]}})
			<-begin
			took[i] = time.Since(start)
			if rec.Code != http.StatusOK {
				t.Errorf("request %d answered %d", i, rec.Code)
			}
		}(i)
		awaitMint(t, arrived, i)
	}
	release := w.stall(t, true, true)
	start = time.Now()
	close(begin)
	close(stalled)
	answered.Wait()
	slowest := slices.Max(took)
	t.Logf("%d requests of %d links, every write stalled: the slowest answered %v after the stall began", requests, adminauth.ResetWindow, slowest)
	if ceiling := 3 * resetFallbackBudget; slowest > ceiling {
		t.Errorf("the slowest request took %v, over %v", slowest, ceiling)
	}
	if n := w.logged.count(lineWithdrawalLost) + w.logged.count(lineRowLost); n != 0 {
		t.Errorf("%d write(s) lost when the requests answered, want none: the retry queue overflowed", n)
	}
	release()
	w.finish(t)
	// Every link that was minted, not requests x window: past its ResetMintLockWait a mint step has a
	// millisecond to get a connection, and one that must be OPENED takes longer and stalls (T236 (a),
	// backlog T309; CI measured 125 of 128). A stall is not this test's subject.
	total := w.mintedCount()
	t.Logf("%d of %d links minted", total, requests*adminauth.ResetWindow)
	if total < requests*adminauth.ResetWindow/2 {
		t.Fatalf("only %d of %d links minted: the queue under test was barely filled", total, requests*adminauth.ResetWindow)
	}
	if n := w.rows(t); n != total {
		t.Errorf("%d row(s), want %d", n, total)
	}
	if n := w.liveLinks(t); n != 0 {
		t.Errorf("%d live link(s), want 0", n)
	}
}

// TestResetOutcomeDB_AnUncertainSendCanLeaveASpendableLinkInTheOwnersBox measures what a lost outcome
// can leave. The channel took the link and answered an error, which is what a lost 250 looks like, and
// both writes then fail on both tries: one copy in the owner's box, one live link, no row, and the link
// SPENDS. It reaches only the owner's own inbox; what is missing is the trail. This is the limit counted
// in ADR 0022's T197 note, not a behaviour wanted: since T212 the list's later withdrawal closes it, so
// the window is until that try (held off here), or ResetTTL when the list is lost.
//
// ⚠️ It pays one shipped-cost bcrypt (the Consume), like TestPanelRecoveryDB_EndToEnd.
func TestResetOutcomeDB_AnUncertainSendCanLeaveASpendableLinkInTheOwnersBox(t *testing.T) {
	mail := &recordingChannel{err: errSendFailed}
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: mail})
	w.flow.clocks.grace = 100 * time.Millisecond
	w.flow.clocks.retryPause = 50 * time.Millisecond
	w.flow.clocks.recoveryFirst = time.Hour // the window before the list's later try (T212)
	var release func()
	w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, true, true) }

	w.request(t)
	waitUntil(t, 20*time.Second, "both lost-write lines", func() bool {
		return w.logged.count(lineWithdrawalLost) == 1 && w.logged.count(lineRowLost) == 1
	})
	release()
	box := mail.all()
	if len(box) != 1 {
		t.Fatalf("%d copies handed to the channel, want 1", len(box))
	}
	if n := w.liveLinks(t); n != 1 {
		t.Errorf("%d live link(s), want 1", n)
	}
	if n := w.rows(t); n != 0 {
		t.Errorf("%d row(s), want 0", n)
	}
	u, err := url.Parse(box[0].Link)
	if err != nil {
		t.Fatal("the link does not parse")
	}
	if _, _, err := w.real.Consume(context.Background(), adminauth.ParseResetToken(u.Query().Get("t")), "the-owners-new-password"); err != nil {
		t.Errorf("Consume of the copy in the box: %v; the limit's sentence says it spends", err)
	}
}

// TestResetOutboxDB_AFullRetryQueueIsAccountedForAtTheDrain measures what the drain's budget does with
// a retry queue far larger than ResetDrainWriteReserve was sized for: the real ResetDrainGrace, the real
// Withdraw and the real recorder on a healthy database. The queue does not have to fit, and the log line
// is the measurement. What must hold is that nothing goes quiet: each write is done or has its ERROR,
// and a drain out of budget says how many it left.
func TestResetOutboxDB_AFullRetryQueueIsAccountedForAtTheDrain(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). Run `make test`.")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(withSmallPool(dsn))})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	tenantID := uuid.New()
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
			VALUES ($1, 'Retry Queue Drain Ltd', $2, 'bar', 'single')`, tenantID, "VAT-"+tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	real, err := adminauth.NewResets(data, adminTestConfig())
	if err != nil {
		t.Fatalf("adminauth.NewResets: %v", err)
	}
	logged := &outcomeLog{}
	resets := &realWithdrawals{fakeResets: &fakeResets{}, real: real}
	h, err := NewAdminReset(resets, refusingChannel{&recordingChannel{}}, trail, adminTestConfig(),
		slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	total := resetRetryQueueSize
	gone := time.Now().Add(-time.Second)
	grants := grantsFor("queued@drain.example.test", total)
	for i := range grants {
		grants[i].Issued.Reset.TenantID = tenantID
		h.recordFallback(ctx, gone, grants[i], resetReasonBreaker)
	}
	if n := logged.count(lineWithdrawalLost) + logged.count(lineRowLost); n != 0 {
		t.Fatalf("CONTROL: %d write(s) lost before the drain, so the queue did not take them all", n)
	}
	dctx, cancel := context.WithTimeout(ctx, ResetDrainGrace)
	defer cancel()
	start := time.Now()
	drainErr := h.Drain(dctx)
	took := time.Since(start)
	select {
	case <-h.outbox.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the worker did not finish after the budget was spent")
	}
	var rows int
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			tenantID, ActionAdminResetUndelivered).Scan(&rows)
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	lostRows, lostWithdrawals := logged.count(lineRowLost), logged.count(lineWithdrawalLost)
	withdrawn := len(resets.withdrawals())
	t.Logf("a retry queue of %d, budget %v (write reserve %v): drain took %v (err %v); %d rows and %d withdrawals written, %d and %d lost with an ERROR",
		total, ResetDrainGrace, ResetDrainWriteReserve, took, drainErr, rows, withdrawn, lostRows, lostWithdrawals)
	// At least, not exactly: a write cut mid-COMMIT can both have happened and be logged as lost.
	if rows+lostRows < total {
		t.Errorf("%d rows + %d lost lines is under %d: a row went quiet", rows, lostRows, total)
	}
	if withdrawn+lostWithdrawals < total {
		t.Errorf("%d withdrawals + %d lost lines is under %d: a withdrawal went quiet", withdrawn, lostWithdrawals, total)
	}
	if rows > total {
		t.Errorf("%d rows, over %d", rows, total)
	}
	if said := logged.count("the drain ended with outcomes still waiting"); (drainErr != nil) != (said == 1) {
		t.Errorf("drain error %v, %d summary line(s): a drain out of budget says so, once, and only then", drainErr, said)
	}
}

// --- the per-identity link: a lock, and no row -------------------------------------------------

// identityStallHook is the real flag-on Resets, with the same pause between mint and writes.
type identityStallHook struct {
	panelResets
	afterMint func(adminauth.PersonResets)
	// down makes WithdrawIdentity answer what a database out of reach answers. applied does the real
	// withdrawal and THEN answers the same (T197's audit: a one-statement autocommit that took effect
	// though the client saw an error). Atomic: the worker reads them while a test flips them.
	down, applied atomic.Bool
}

func (h *identityStallHook) WithdrawIdentity(ctx context.Context, t adminauth.ResetToken) error {
	if h.down.Load() {
		return fmt.Errorf("adminauth: withdraw identity reset: %w", context.DeadlineExceeded)
	}
	if h.applied.Load() {
		if err := h.panelResets.WithdrawIdentity(ctx, t); err != nil {
			return err
		}
		return fmt.Errorf("adminauth: withdraw identity reset: %w", context.DeadlineExceeded)
	}
	return h.panelResets.WithdrawIdentity(ctx, t)
}

func (h *identityStallHook) IssueForPerson(ctx context.Context, email string) (adminauth.PersonResets, error) {
	p, err := h.panelResets.IssueForPerson(ctx, email)
	if err == nil && h.afterMint != nil && len(p.Grants) > 0 {
		h.afterMint(p)
	}
	return p, err
}

// identityStall locks the identity's live link row on a connection of its own.
func identityStall(t *testing.T, ident uuid.UUID) (release func()) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM admin_identity_resets WHERE identity_id = $1 AND cancelled_at IS NULL FOR UPDATE`, ident); err != nil {
		t.Fatalf("lock the identity's link: %v", err)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if err := tx.Rollback(context.Background()); err != nil {
				t.Errorf("rollback: %v", err)
			}
			if err := c.Close(context.Background()); err != nil {
				t.Errorf("close: %v", err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

// identityOutcomeHarness is identityResetHarness with this flow's channel swapped and a logger over a
// buffer, before the first request.
func identityOutcomeHarness(t *testing.T, ch ResetChannel) (*panelHarness, *pgx.Conn, uuid.UUID, *identityStallHook, *outcomeLog) {
	t.Helper()
	p, owner, ident, _ := identityResetHarness(t)
	logged := &outcomeLog{}
	hook := &identityStallHook{panelResets: p.reset.resets}
	p.reset.resets = hook
	p.reset.mail = ch
	p.reset.log = slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return p, owner, ident, hook, logged
}

func (p *panelHarness) askReset(t *testing.T, email string) {
	t.Helper()
	_, page := p.get(t, adminResetPath)
	if res, _ := p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, page)}, "email": {email}}); res.StatusCode != 200 {
		t.Fatalf("the request answered %d", res.StatusCode)
	}
}

func liveIdentityLinks(t *testing.T, owner *pgx.Conn, ident uuid.UUID) int {
	t.Helper()
	var n int
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1 AND cancelled_at IS NULL AND used_at IS NULL`, ident).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestPanelIdentityReset_AStalledWithdrawalIsTriedOnceMore: flag on, the deployed state
// (deploy/k8s/05-config.yaml). The fallback's one write stalls, the answer leaves inside the budget,
// the worker's second try withdraws the link, the person can ask again at once. No audit row on any
// path: the identity has no tenant (ADR 0031 §5.2), so the trail is the one log line.
//
// MUTATION: no second try (resetOutcomeTries 1) -> the link stays -> red.
func TestPanelIdentityReset_AStalledWithdrawalIsTriedOnceMore(t *testing.T) {
	mail := &recordingChannel{}
	p, owner, ident, hook, logged := identityOutcomeHarness(t, refusingChannel{mail})
	var release func()
	var mintedAt time.Time
	hook.afterMint = func(adminauth.PersonResets) {
		release = identityStall(t, ident)
		mintedAt = time.Now()
	}

	p.askReset(t, p.email)
	took := time.Since(mintedAt)
	t.Logf("one identity link, its withdrawal stalled: answered %v after the mint (step %v)", took, resetFallbackStep)
	if ceiling := 3 * resetFallbackBudget; took > ceiling {
		t.Errorf("the response took %v after the mint, over %v", took, ceiling)
	}
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Fatalf("CONTROL: %d live link(s) while the lock is held, want 1", n)
	}
	release()
	waitUntil(t, 20*time.Second, "the second try's withdrawal", func() bool { return liveIdentityLinks(t, owner, ident) == 0 })

	hook.afterMint = nil
	p.reset.mail = mail
	p.askReset(t, p.email)
	waitUntil(t, 20*time.Second, "a fresh link delivered", func() bool { return len(mail.all()) == 1 })
	for _, a := range []string{ActionAdminResetUndelivered, ActionAdminResetRequested, ActionAdminResetLimited} {
		if n := p.auditCount(t, a); n != 0 {
			t.Errorf("%d %s row(s) for a per-identity link, want 0", n, a)
		}
	}
	if n := logged.count(lineIdentityLost); n != 0 {
		t.Errorf("%d lost-withdrawal line(s), want none:\n%s", n, logged)
	}
	if n := logged.count("panel recovery: identity link outcome"); n != 2 {
		t.Errorf("%d outcome line(s), want one per link, however many tries:\n%s", n, logged)
	}
}

// TestPanelIdentityReset_BothTriesFailingLockThePersonOut is T197's sharpest case, measured: both tries
// fail, the one ERROR line is written with ids only, and the person's next request is "kept" -- no
// link in ANY of their businesses until ResetTTL (M14-14's audit). The link is usually in no inbox;
// after an uncertain send it can be live and spendable in the owner's inbox
// (TestResetOutcomeDB_AnUncertainSendCanLeaveASpendableLinkInTheOwnersBox). Since T212 this is the
// state until the list of withdrawals owed makes the withdrawal (held off here;
// TestPanelIdentityReset_ALateWithdrawalUnlocksThePerson measures the end of it), and until ResetTTL
// only when the list is lost (full, closing, the process dead).
//
// THE FAILURE IS THE HOOK'S, NOT A LOCK'S: a lock made this test flaky. identity_withdraw_reset is one
// autocommitted statement, and with the lock released right after the ERROR line the link was found
// dead in 1 run of 34 (the other 33 stayed live; the likely cause, read and not measured, is the lock
// going before pgconn's asyncClose delivers its cancel). That is the safe direction, and the person is
// locked out when the statement never takes effect, which is what the hook answers.
func TestPanelIdentityReset_BothTriesFailingLockThePersonOut(t *testing.T) {
	mail := &recordingChannel{err: errSendFailed}
	p, owner, ident, hook, logged := identityOutcomeHarness(t, mail)
	p.reset.clocks.retryPause = 50 * time.Millisecond
	p.reset.clocks.recoveryFirst = time.Hour // the state before the list's later try (resetrecovery_db_test.go)
	hook.down.Store(true)

	p.askReset(t, p.email)
	waitUntil(t, 20*time.Second, "the lost-withdrawal line", func() bool { return logged.count(lineIdentityLost) == 1 })
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Errorf("%d live link(s), want 1", n)
	}
	if strings.Contains(strings.ToLower(logged.String()), strings.ToLower(p.email)) {
		t.Error("a line carries the address")
	}
	mail.mu.Lock()
	mail.err = nil
	mail.mu.Unlock()
	before := len(mail.all())
	p.askReset(t, p.email)
	if logged.count("panel recovery: identity link kept") != 1 {
		t.Errorf("the next request was not kept:\n%s", logged)
	}
	p.drainReset(t)
	if n := len(mail.all()); n != before {
		t.Errorf("%d delivery attempt(s) after the lost withdrawal, want still %d", n, before)
	}
	if n := p.auditCount(t, ActionAdminResetUndelivered); n != 0 {
		t.Errorf("%d undelivered row(s) for a per-identity link, want 0", n)
	}
}

// --- the proxy ---------------------------------------------------------------------------------

// faultProxy sits between the pool and Postgres: down() cuts every connection and answers new ones by
// closing them, loseCommitAnswer lets a COMMIT reach the server and drops its answer.
type faultProxy struct {
	t      *testing.T
	ln     net.Listener
	target string

	mu     sync.Mutex
	isDown bool
	conns  map[net.Conn]struct{}
	armed  bool
	skip   int
	lost   bool
}

func newFaultProxy(t *testing.T) *faultProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &faultProxy{t: t, ln: ln, conns: map[net.Conn]struct{}{}}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close the proxy: %v", err)
		}
		p.closeAll()
	})
	return p
}

func (p *faultProxy) dsn(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse the dsn: %v", err)
	}
	p.target = u.Host
	u.Host = p.ln.Addr().String()
	go p.serve()
	return u.String()
}

func (p *faultProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		down := p.isDown
		p.mu.Unlock()
		if down {
			_ = c.Close()
			continue
		}
		s, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.mu.Lock()
		p.conns[c], p.conns[s] = struct{}{}, struct{}{}
		p.mu.Unlock()
		swallow := make(chan struct{})
		go p.toServer(c, s, swallow)
		go p.toClient(s, c, swallow)
	}
}

// toServer forwards the client's bytes, and marks the answer to an armed COMMIT as one to drop.
func (p *faultProxy) toServer(c, s net.Conn, swallow chan struct{}) {
	buf := make([]byte, 32*1024)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			if p.commitToLose(buf[:n]) {
				close(swallow)
			}
			if _, werr := s.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			_ = s.Close()
			return
		}
	}
}

// toClient forwards the server's bytes until an answer is to be dropped: it reads that answer, so the
// COMMIT has certainly happened, and closes both ends without relaying it.
func (p *faultProxy) toClient(s, c net.Conn, swallow chan struct{}) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.Read(buf)
		select {
		case <-swallow:
			_ = c.Close()
			_ = s.Close()
			return
		default:
		}
		if n > 0 {
			if _, werr := c.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				_ = s.Close()
			}
			_ = c.Close()
			return
		}
	}
}

func (p *faultProxy) commitToLose(b []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.armed || !bytes.Contains(bytes.ToLower(b), []byte("commit\x00")) {
		return false
	}
	if p.skip > 0 {
		p.skip--
		return false
	}
	p.armed, p.lost = false, true
	return true
}

func (p *faultProxy) down() {
	p.mu.Lock()
	p.isDown = true
	p.mu.Unlock()
	p.closeAll()
}

func (p *faultProxy) up() {
	p.mu.Lock()
	p.isDown = false
	p.mu.Unlock()
}

func (p *faultProxy) loseCommitAnswer(skip int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.armed, p.skip = true, skip
}

func (p *faultProxy) lostAnswer() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lost
}

func (p *faultProxy) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		_ = c.Close()
		delete(p.conns, c)
	}
}

// hookedHandler is a slog.Handler that calls on for every record before handling it.
type hookedHandler struct {
	slog.Handler
	on func(slog.Record)
}

func (h hookedHandler) Handle(ctx context.Context, r slog.Record) error {
	h.on(r)
	return h.Handler.Handle(ctx, r)
}

func recordHas(r slog.Record, key, value string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key && a.Value.String() == value {
			found = true
			return false
		}
		return true
	})
	return found
}
