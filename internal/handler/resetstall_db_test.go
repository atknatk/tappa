package handler

// resetstall_db_test.go -- T211 and T219 against real Postgres, over the whole request: a lock held in
// another session stalls the kept row's INSERT (T211) or the mint (T219), and the answer leaves at the
// floor with the status and the body of an unknown address. Every lock is released by a timer as well,
// so a mutation that removes a bound waits for the timer and is measured, instead of hanging.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
)

// timedRequest drives the form once and times the POST alone.
func timedRequest(t *testing.T, h http.Handler, email string) (code int, body string, took time.Duration) {
	t.Helper()
	b := newBrowser(t, h)
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
	start := time.Now()
	rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
	return rec.Code, rec.Body.String(), time.Since(start)
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

// TestResetOutcomeDB_AStalledKeptRowAnswersAtTheFloor (T211): a full adminauth.ResetWindow whose
// accounts hold live links, the businesses' rows locked so every kept INSERT waits (audit_log's foreign
// key, 00005). The answer leaves at the floor, where an unknown address's does; once the lock goes the
// worker writes every row, and no try touches the live links. Before T211 the same setup answered after
// 40.05 s and wrote no row (one resetAuditGrace per account, no second try).
//
// The ceiling is the floor plus one fallback write, between the floor (measured 250.7 ms) and the
// shared fallback budget's answer (measured 1.18 s), so it tells the two clocks apart without reading
// the machine's load. MUTATIONS: the kept rows on resetFallbackBudget -> about 1.2 s -> red. recordKept
// as before T211 -> 40 s, 0 rows -> red. A kept outcome owing a withdrawal -> 0 live links -> red.
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
	release := w.stall(t, false, true)
	releaseAfter(60*time.Second, release)
	code, _, took := timedRequest(t, w.router, w.email)
	kept := w.count(t, ActionAdminResetKept)
	release()
	t.Logf("%d accounts, every kept row stalled: %d in %v (floor %v); %d row(s) when it answered", n, code, took, resetRequestFloor, kept)
	if code != http.StatusOK {
		t.Fatalf("the request answered %d", code)
	}
	if ceiling := resetRequestFloor + resetFallbackStep; took > ceiling {
		t.Errorf("the request took %v, over %v: the stalled kept rows are read from the clock", took, ceiling)
	}
	if kept != 0 {
		t.Fatalf("CONTROL: %d kept row(s) while the lock was held, want 0: the rows were not stalled", kept)
	}
	w.finish(t)
	if got := w.count(t, ActionAdminResetKept); got != n {
		t.Errorf("%d kept row(s) after the lock went, want %d: the stall cost a record (§4.6)", got, n)
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
// address's -- status, body, and the floor's clock -- with no link left behind and one stalled-mint
// line; once the lock goes, the owner's next request gets a link. Before T219 the same setup answered
// 500 at the request's deadline (6.0 s in the measurement, 30 s in production).
//
// MUTATION: no lock_timeout in the mint (adminauth's BoundLockWaits removed) -> the request waits for
// the 3 s timer -> red.
func TestPanelRecoveryDB_AStalledMintAnswersLikeAnUnknownAddress(t *testing.T) {
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: &recordingChannel{}})
	wantCode, wantBody, _ := timedRequest(t, w.router, "nobody-"+uuid.NewString()+"@outcome.example")

	release := w.stall(t, false, true)
	releaseAfter(3*time.Second, release)
	code, body, took := timedRequest(t, w.router, w.email)
	release()
	t.Logf("the business row locked before the request: %d in %v (floor %v)", code, took, resetRequestFloor)
	if code != wantCode || body != wantBody {
		t.Fatalf("a stalled registered address answered %d (%d bytes), an unknown one %d (%d bytes)", code, len(body), wantCode, len(wantBody))
	}
	if ceiling := time.Second; took > ceiling {
		t.Errorf("the request took %v, over %v: the mint waited for the lock", took, ceiling)
	}
	if n := w.liveLinks(t); n != 0 {
		t.Fatalf("%d live link(s) after a stalled mint, want 0", n)
	}
	if n := w.logged.count(lineStalledMint); n != 1 {
		t.Errorf("%d stalled-mint line(s), want 1:\n%s", n, w.logged)
	}
	w.request(t)
	if n := w.liveLinks(t); n != 1 {
		t.Errorf("%d live link(s) after asking again with the lock gone, want 1: the stall locked the owner out", n)
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
// request. The answer is the unknown address's, no identity link is opened, the stall is one line with
// the identity's id; once the lock goes, the next request opens the link. Before T219: 500 at the
// request's deadline (6.02 s in the measurement).
//
// MUTATION: no lock_timeout in OpenIdentityReset -> the request waits for the 3 s timer -> red.
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
	code, body, took := post(p.email)
	release()
	t.Logf("the identity's lock held before the request: %d in %v (floor %v)", code, took, resetRequestFloor)
	if code != wantCode || body != wantBody {
		t.Fatalf("a stalled identity answered %d (%d bytes), an unknown address %d (%d bytes)", code, len(body), wantCode, len(wantBody))
	}
	if ceiling := time.Second; took > ceiling {
		t.Errorf("the request took %v, over %v: the opening waited for the lock", took, ceiling)
	}
	if n := liveIdentityLinks(t, owner, ident); n != 0 {
		t.Fatalf("%d live identity link(s) after a stalled opening, want 0", n)
	}
	if n := logged.count(lineStalledMint); n != 1 {
		t.Errorf("%d stalled-mint line(s), want 1:\n%s", n, logged)
	}
	if n := logged.count(`identity_id=` + ident.String()); n == 0 {
		t.Errorf("no line names the identity:\n%s", logged)
	}
	p.askReset(t, p.email)
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Errorf("%d live identity link(s) after asking again with the lock gone, want 1", n)
	}
}
