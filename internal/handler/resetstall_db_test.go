package handler

// resetstall_db_test.go -- T211 and T219 against real Postgres, over the whole request: a lock held in
// another session stalls the kept row's INSERT (T211) or the mint (T219), and the answer stays in the
// timing band every other address answers in -- [floor, floor+50ms] at its fastest of stallRounds,
// the band of TestResetOutboxDB_TheFallbackWithdrawalsStayUnderTheFloor, CI the referee (T198) --
// with the status and the body of an unknown address. Every lock is released by a timer as well, so
// a mutation that removes a bound waits for the timer and is measured, instead of hanging.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
)

// stallRounds is how many stalled requests each test times: liveLinkRounds' reason (one pause is never
// an arm's fastest sample).
const stallRounds = liveLinkRounds

// stallBand is the ceiling of a stalled answer's fastest round: the floor's band, the same for every arm.
const stallBand = resetRequestFloor + 50*time.Millisecond

// timedRequest drives the form once and times the POST alone.
func timedRequest(t *testing.T, h http.Handler, email string) (code int, body string, took time.Duration) {
	t.Helper()
	b := newBrowser(t, h)
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
	start := time.Now()
	rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
	return rec.Code, rec.Body.String(), time.Since(start)
}

// stalledRounds times stallRounds requests for email and fails on any answer that differs from the
// unknown address's; it returns the samples.
func stalledRounds(t *testing.T, post func(string) (int, string, time.Duration), email, wantBody string, wantCode int) []time.Duration {
	t.Helper()
	took := make([]time.Duration, 0, stallRounds)
	for i := 0; i < stallRounds; i++ {
		code, body, d := post(email)
		if code != wantCode || body != wantBody {
			t.Fatalf("round %d: a stalled registered address answered %d (%d bytes), an unknown one %d (%d bytes)",
				i+1, code, len(body), wantCode, len(wantBody))
		}
		took = append(took, d)
	}
	return took
}

func checkBand(t *testing.T, what string, took []time.Duration) {
	t.Helper()
	fastest := minDuration(took)
	t.Logf("%s: answered in %v (fastest %v, floor %v)", what, took, fastest, resetRequestFloor)
	if fastest > stallBand {
		t.Errorf("%s: the fastest of %d rounds answered in %v, over floor+50ms (%v): the stall is read from the clock",
			what, len(took), fastest, stallBand)
	}
}

// releaseAfter calls release after d unless it was called first (release is idempotent).
func releaseAfter(d time.Duration, release func()) {
	go func() {
		time.Sleep(d)
		release()
	}()
}

func (w *outcomeWorld) count(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := w.owner.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE tenant_id = ANY($1) AND action = $2`, w.tenants, action).Scan(&n); err != nil {
		t.Fatalf("count the %s rows: %v", action, err)
	}
	return n
}

func (w *outcomeWorld) post(t *testing.T) func(string) (int, string, time.Duration) {
	return func(email string) (int, string, time.Duration) { return timedRequest(t, w.router, email) }
}

// TestResetOutcomeDB_AStalledKeptRowAnswersAtTheFloor (T211): a full adminauth.ResetWindow whose
// accounts hold live links, the businesses' rows locked so every kept INSERT waits (audit_log's foreign
// key, 00005). The answer stays in the floor's band; once the lock goes the worker writes every row,
// and no try touches the live links. Before T211 the same setup answered after 40.05 s and wrote no row
// (one resetAuditGrace per account, no second try).
//
// MUTATIONS: the kept rows on resetFallbackBudget -> about 1.1 s -> red. recordKept as before T211 ->
// 40 s, 0 rows -> red. A kept outcome owing a withdrawal -> 0 live links -> red.
func TestResetOutcomeDB_AStalledKeptRowAnswersAtTheFloor(t *testing.T) {
	n := adminauth.ResetWindow
	w := newOutcomeWorld(t, outcomeWorldOptions{n: n, channel: &recordingChannel{}})
	w.request(t)
	deadline := time.Now().Add(15 * time.Second)
	for w.count(t, ActionAdminResetRequested) < n {
		if time.Now().After(deadline) {
			t.Fatalf("CONTROL: %d of %d links delivered", w.count(t, ActionAdminResetRequested), n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	wantCode, wantBody, _ := timedRequest(t, w.router, "nobody-"+uuid.NewString()+"@outcome.example")
	release := w.stall(t, false, true)
	releaseAfter(5*time.Minute, release)
	took := stalledRounds(t, w.post(t), w.email, wantBody, wantCode)
	kept := w.count(t, ActionAdminResetKept)
	release()
	checkBand(t, "8 accounts, every kept row stalled", took)
	if kept != 0 {
		t.Fatalf("CONTROL: %d kept row(s) while the lock was held, want 0: the rows were not stalled", kept)
	}
	w.finish(t)
	if got, want := w.count(t, ActionAdminResetKept), n*stallRounds; got != want {
		t.Errorf("%d kept row(s) after the lock went, want %d: the stall cost a record (§4.6)", got, want)
	}
	if got := w.liveLinks(t); got != n {
		t.Errorf("%d live link(s), want %d: a kept row's retry retired a link its owner holds", got, n)
	}
	if got := w.logged.count(lineRowLost) + w.logged.count(lineWithdrawalLost); got != 0 {
		t.Errorf("%d lost-write line(s), want none:\n%s", got, w.logged)
	}
}

// TestPanelRecoveryDB_AStalledMintAnswersLikeAnUnknownAddress (T219, flag off): the business row is
// locked BEFORE the request, so the mint's INSERT waits on its foreign key. The answer is the unknown
// address's -- status, body, and the floor's band -- with no link left behind and a stalled-mint line
// per request; once the lock goes, the owner's next request gets a link. Before T219 the same setup
// answered 500 at the request's deadline (6.0 s in the measurement, 30 s in production).
//
// MUTATIONS: no lock_timeout in the mint -> the first request waits for the 3 s timer -> red.
// adminauth.ResetMintLockWait raised to 900 ms -> about 910 ms -> red.
func TestPanelRecoveryDB_AStalledMintAnswersLikeAnUnknownAddress(t *testing.T) {
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: &recordingChannel{}})
	wantCode, wantBody, _ := timedRequest(t, w.router, "nobody-"+uuid.NewString()+"@outcome.example")

	release := w.stall(t, false, true)
	releaseAfter(3*time.Second, release)
	took := stalledRounds(t, w.post(t), w.email, wantBody, wantCode)
	release()
	checkBand(t, "the business row locked before the request", took)
	if n := w.liveLinks(t); n != 0 {
		t.Fatalf("%d live link(s) after stalled mints, want 0", n)
	}
	if n := w.logged.count(lineStalledMint); n != stallRounds {
		t.Errorf("%d stalled-mint line(s), want %d:\n%s", n, stallRounds, w.logged)
	}
	w.request(t)
	if n := w.liveLinks(t); n != 1 {
		t.Errorf("%d live link(s) after asking again with the lock gone, want 1: the stall locked the owner out", n)
	}
	w.finish(t)
}

// TestPanelRecoveryDB_AStalledFullWindowAnswersLikeAnUnknownAddress (T219, the third eye's full
// window): a full adminauth.ResetWindow behind one address, every business row locked, so all eight
// mints stall. The eight share one ResetMintLockWait, so the answer stays in the floor's band like one
// account's, and nothing is left behind.
//
// MUTATIONS: the budget per transaction (no request clock) -> 8 x 150 ms -> red. ResetMintLockWait
// raised to 900 ms -> red.
func TestPanelRecoveryDB_AStalledFullWindowAnswersLikeAnUnknownAddress(t *testing.T) {
	n := adminauth.ResetWindow
	w := newOutcomeWorld(t, outcomeWorldOptions{n: n, channel: &recordingChannel{}})
	wantCode, wantBody, _ := timedRequest(t, w.router, "nobody-"+uuid.NewString()+"@outcome.example")

	release := w.stall(t, false, true)
	releaseAfter(30*time.Second, release)
	took := stalledRounds(t, w.post(t), w.email, wantBody, wantCode)
	release()
	checkBand(t, "a full window, every business row locked", took)
	if got := w.liveLinks(t); got != 0 {
		t.Fatalf("%d live link(s) after stalled mints, want 0", got)
	}
	w.request(t)
	if got := w.liveLinks(t); got != n {
		t.Errorf("%d live link(s) after asking again with the locks gone, want %d", got, n)
	}
	w.finish(t)
}

// holdAdvisoryLock takes a transaction-scoped advisory lock on a connection of its own and returns its
// idempotent release.
func holdAdvisoryLock(t *testing.T, key string) (release func()) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatalf("connect the lock holder: %v", err)
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the lock holder: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if err := tx.Rollback(context.Background()); err != nil {
				t.Errorf("release the lock: %v", err)
			}
			if err := c.Close(context.Background()); err != nil {
				t.Errorf("close the lock holder: %v", err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

// TestPanelIdentityReset_AStalledOpeningAnswersLikeAnUnknownAddress (T219, flag on, the deployed
// state): the identity's opening lock (identity_open_reset's advisory key, 00072) is held BEFORE the
// request. The answer is the unknown address's, in the floor's band, no identity link is opened, each
// stall is one line with the identity's id; once the lock goes, the next request opens the link.
// Before T219: 500 at the request's deadline (6.02 s in the measurement).
//
// MUTATIONS: no lock_timeout in OpenIdentityReset -> the first request waits for the 3 s timer -> red.
// ResetMintLockWait raised to 900 ms -> red.
func TestPanelIdentityReset_AStalledOpeningAnswersLikeAnUnknownAddress(t *testing.T) {
	p, owner, ident, _, logged := identityOutcomeHarness(t, &recordingChannel{})
	post := func(email string) (int, string, time.Duration) {
		_, page := p.get(t, adminResetPath)
		start := time.Now()
		res, body := p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, page)}, "email": {email}})
		return res.StatusCode, body, time.Since(start)
	}
	wantCode, wantBody, _ := post("nobody-" + uuid.NewString() + "@outcome.example")

	release := holdAdvisoryLock(t, "admin-identity-reset:"+ident.String())
	releaseAfter(3*time.Second, release)
	took := stalledRounds(t, post, p.email, wantBody, wantCode)
	release()
	checkBand(t, "the identity's lock held before the request", took)
	if n := liveIdentityLinks(t, owner, ident); n != 0 {
		t.Fatalf("%d live identity link(s) after stalled openings, want 0", n)
	}
	if n := logged.count(lineStalledMint); n != stallRounds {
		t.Errorf("%d stalled-mint line(s), want %d:\n%s", n, stallRounds, logged)
	}
	if n := logged.count(`identity_id=` + ident.String()); n == 0 {
		t.Errorf("no line names the identity:\n%s", logged)
	}
	p.askReset(t, p.email)
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Errorf("%d live identity link(s) after asking again with the lock gone, want 1", n)
	}
}

// TestResetOutcomeDB_KeptRowsCannotCrowdOutAnUndeliveredOutcome re-measures resetRetryQueueSize with the
// kept rows in it (T211's security review). TestResetOutcomeDB_AShortStallDoesNotOverflowTheRetryQueue's
// 16 full windows of undelivered outcomes, behind a stall -- but first a flood of kept rows into the
// same stall, more than half the queue (24 full windows, 192 rows). The kept rows stop at their share;
// every undelivered outcome still fits, so no undelivered row is lost and no link stays live. The kept
// rows past their share are lost with their ERROR, and counted.
//
// MUTATION: no kept share (offerRetry takes kept rows to the full queue) -> undelivered outcomes
// refused -> rows lost, links live -> red.
func TestResetOutcomeDB_KeptRowsCannotCrowdOutAnUndeliveredOutcome(t *testing.T) {
	const undeliveredRequests, keptRequests = 16, 24
	win := adminauth.ResetWindow
	w := newOutcomeWorld(t, outcomeWorldOptions{n: win, addresses: undeliveredRequests + keptRequests,
		channel: refusingChannel{&recordingChannel{}}})
	ctx := context.Background()
	keptEmails := w.emails[undeliveredRequests:]
	for _, e := range keptEmails {
		// A live link per account, minted past the flow so nothing withdraws it: the kept requests' arm.
		if g, _, _, err := w.real.IssueForEmail(ctx, e); err != nil || len(g) != win {
			t.Fatalf("seed the live links: %v, %d grant(s)", err, len(g))
		}
	}
	liveKept := w.liveLinks(t)

	allMinted, stalled := make(chan struct{}), make(chan struct{})
	var arrived sync.WaitGroup
	arrived.Add(undeliveredRequests)
	go func() {
		arrived.Wait()
		close(allMinted)
	}()
	w.afterMint = func([]adminauth.ResetGrant) {
		arrived.Done()
		<-stalled
	}
	var answered sync.WaitGroup
	for i := 0; i < undeliveredRequests; i++ {
		b := newBrowser(t, w.router)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
		answered.Add(1)
		go func(email string) {
			defer answered.Done()
			if rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}}); rec.Code != http.StatusOK {
				t.Errorf("an undelivered request answered %d", rec.Code)
			}
		}(w.emails[i])
	}
	<-allMinted
	release := w.stall(t, true, true)
	releaseAfter(5*time.Minute, release)

	// The flood first: every kept row stalls, goes to the queue, and stops at its share.
	var kept sync.WaitGroup
	keptTook := make([]time.Duration, keptRequests)
	for i, e := range keptEmails {
		kept.Add(1)
		// A browser of its own per request, from its own address: the request budget is per source.
		b := newBrowser(t, w.router)
		b.ip = fmt.Sprintf("198.51.100.%d:1234", i+1)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
		go func(i int, e string) {
			defer kept.Done()
			start := time.Now()
			if rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {e}}); rec.Code != http.StatusOK {
				t.Errorf("a kept request answered %d", rec.Code)
			}
			keptTook[i] = time.Since(start)
		}(i, e)
	}
	kept.Wait()
	keptLost := w.logged.count(lineRowLost)
	close(stalled)
	answered.Wait()
	t.Logf("%d kept rows offered into the stall, %d lost past their share (%d); slowest kept answer %v",
		keptRequests*win, keptLost, resetRetryKeptShare, slices.Max(keptTook))
	if lost := w.logged.count(lineRowLost) - keptLost; lost != 0 {
		t.Errorf("%d undelivered row(s) lost when the requests answered, want none: kept rows crowded them out", lost)
	}
	if n := w.logged.count(lineWithdrawalLost); n != 0 {
		t.Errorf("%d withdrawal(s) lost, want none", n)
	}
	release()
	w.finish(t)
	if n, want := w.rows(t), undeliveredRequests*win; n != want {
		t.Errorf("%d undelivered row(s), want %d", n, want)
	}
	if n := w.liveLinks(t); n != liveKept {
		t.Errorf("%d live link(s), want %d (the seeded ones): an undelivered link stayed live", n, liveKept)
	}
	if keptLost == 0 {
		t.Errorf("CONTROL: no kept row was lost, so the flood never reached its share and the test proves nothing")
	}
}
