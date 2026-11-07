package handler

// resetrecovery_db_test.go -- T212 against real Postgres: a withdrawal whose two tries failed waits on
// the list of withdrawals owed, and the worker makes it once the database answers. The per-row link is
// stalled by a row lock taken in another session; the identity link by the hook that answers what a
// database out of reach answers (a lock made the identity test flaky, TestPanelIdentityReset_
// BothTriesFailingLockThePersonOut says why).

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// Lines of the list.
const (
	lineOwedRecovered = "an owed withdrawal succeeded on a later try"
	lineOwedAgain     = "an owed withdrawal failed again and is tried later"
	lineOwedExpired   = "an owed withdrawal was dropped untried, its link has expired"
	lineOwedAtDrain   = "the process is stopping with withdrawals still owed"
)

// recoveryClocks are short clocks for the list: a later try 200 ms after the loss, passes at most
// 400 ms apart.
func recoveryClocks(h *AdminReset) {
	h.clocks.grace = 100 * time.Millisecond
	h.clocks.retryPause = 50 * time.Millisecond
	h.clocks.recoveryFirst = 200 * time.Millisecond
	h.clocks.recoveryMaxPause = 400 * time.Millisecond
	h.clocks.recoveryPass = time.Second
}

// dueNow makes every owed withdrawal due and wakes the worker: a test that must put something between
// the loss and the later try sets recoveryFirst to an hour and calls this when it is ready.
func dueNow(h *AdminReset) {
	q := h.outbox
	q.mu.Lock()
	for _, w := range q.owed {
		w.notBefore = time.Now()
	}
	q.mu.Unlock()
	select {
	case q.owedKick <- struct{}{}:
	default:
	}
}

func (w *outcomeWorld) linkIsLive(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var live bool
	if err := w.owner.QueryRow(context.Background(),
		`SELECT cancelled_at IS NULL AND used_at IS NULL FROM password_resets WHERE id = $1`, id).Scan(&live); err != nil {
		t.Fatalf("read the link: %v", err)
	}
	return live
}

// TestResetRecoveryDB_ALockedLinkIsWithdrawnOnceTheLockGoes is T212 on the per-row path, the worker's
// outcome: the relay fails, the link's row is locked, both tries of the withdrawal fail and so does the
// list's first later try. The link is live while the lock holds. The lock goes; the list's next try
// withdraws it without anybody asking, and the owner's next request mints and delivers a fresh link.
// Before T212 the link stayed live for ResetTTL and every request of the owner was "kept".
//
// MUTATION: the list taken away (owe always refusing) -> the link stays live -> red.
func TestResetRecoveryDB_ALockedLinkIsWithdrawnOnceTheLockGoes(t *testing.T) {
	mail := &recordingChannel{err: errSendFailed}
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, channel: mail})
	recoveryClocks(w.flow)
	var release func()
	w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, true, false) }

	w.request(t)
	waitUntil(t, 20*time.Second, "the lost withdrawal, owed", func() bool {
		return w.logged.count(lineWithdrawalLost) == 1 && w.logged.count("recovery=retrying") == 1
	})
	waitUntil(t, 20*time.Second, "a later try failing under the lock", func() bool { return w.logged.count(lineOwedAgain) >= 1 })
	if n := w.liveLinks(t); n != 1 {
		t.Fatalf("CONTROL: %d live link(s) while the lock is held, want 1", n)
	}
	release()
	waitUntil(t, 20*time.Second, "the list's withdrawal", func() bool { return w.logged.count(lineOwedRecovered) == 1 })
	if n := w.liveLinks(t); n != 0 {
		t.Fatalf("%d live link(s) after the later try, want 0", n)
	}

	mail.mu.Lock()
	mail.err = nil
	mail.mu.Unlock()
	w.afterMint = nil
	w.minted = nil
	w.request(t)
	if len(w.minted) != 1 {
		t.Fatalf("the owner's next request minted %d link(s), want 1", len(w.minted))
	}
	waitUntil(t, 20*time.Second, "the fresh link delivered", func() bool { return len(mail.all()) == 2 })
	if n := w.liveLinks(t); n != 1 {
		t.Errorf("%d live link(s), want the fresh one", n)
	}
	if n := w.rows(t); n != 1 {
		t.Errorf("%d undelivered row(s), want the first link's 1: a later withdrawal writes no row", n)
	}
	if strings.Contains(w.logged.String(), w.email) {
		t.Error("a line carries the address")
	}
}

// TestResetRecoveryDB_ALateWithdrawalKillsOnlyItsOwnLink is the target rule, measured: by the time the
// list's try runs, the owed link (A1) is already gone -- the earlier try took effect though its client
// saw an error, T197's finding -- the same owner holds a NEWER link (A2), and another administrator of
// the same business holds his own (B). The later try withdraws A1 again, which moves nothing, succeeds,
// and leaves A2 and B live. So a second withdrawal is harmless, and a late one kills nothing it was
// not handed.
//
// MUTATIONS: the withdrawal keyed by the account (the owner's newest link) -> A2 dies -> red; keyed by
// the business -> A2 and B die -> red.
func TestResetRecoveryDB_ALateWithdrawalKillsOnlyItsOwnLink(t *testing.T) {
	mail := &recordingChannel{}
	w := newOutcomeWorld(t, outcomeWorldOptions{n: 1, addresses: 2, channel: mail})
	recoveryClocks(w.flow)
	w.flow.clocks.recoveryFirst = time.Hour // the later try waits for dueNow
	owner, other := w.emails[0], w.emails[1]

	requestReset(t, newBrowser(t, w.router), other)
	waitUntil(t, 20*time.Second, "B's link delivered", func() bool { return len(mail.all()) == 1 })
	b := w.minted[0].Issued.Reset.ID

	mail.mu.Lock()
	mail.err = errSendFailed
	mail.mu.Unlock()
	w.minted = nil
	var release func()
	w.afterMint = func([]adminauth.ResetGrant) { release = w.stall(t, true, false) }
	requestReset(t, newBrowser(t, w.router), owner)
	waitUntil(t, 20*time.Second, "A1's withdrawal, owed", func() bool { return w.logged.count("recovery=retrying") == 1 })
	a1 := w.minted[0].Issued.Reset.ID
	release()
	// The earlier try that took effect after all.
	if _, err := w.owner.Exec(context.Background(), `UPDATE password_resets SET cancelled_at = now() WHERE id = $1`, a1); err != nil {
		t.Fatalf("retire A1: %v", err)
	}

	mail.mu.Lock()
	mail.err = nil
	mail.mu.Unlock()
	w.afterMint = nil
	w.minted = nil
	requestReset(t, newBrowser(t, w.router), owner)
	if len(w.minted) != 1 {
		t.Fatalf("PREMISE: the owner's request minted %d link(s), want A2", len(w.minted))
	}
	a2 := w.minted[0].Issued.Reset.ID
	waitUntil(t, 20*time.Second, "A2 delivered", func() bool { return len(mail.all()) == 3 })

	dueNow(w.flow)
	waitUntil(t, 20*time.Second, "the list's try of A1", func() bool { return w.logged.count(lineOwedRecovered) == 1 })
	if w.linkIsLive(t, a1) {
		t.Error("A1 is live")
	}
	if !w.linkIsLive(t, a2) {
		t.Error("A2, the owner's newer link, was killed by the late withdrawal of A1")
	}
	if !w.linkIsLive(t, b) {
		t.Error("B's link was killed by the late withdrawal of A's")
	}
}

// TestPanelIdentityReset_ALateWithdrawalUnlocksThePerson is T212 on the identity link, flag on (the
// deployed state): both tries fail and so does a later one, the person's next request would be "kept"
// in every business; the database comes back, the list withdraws the link, and the person's next
// request is delivered a fresh one. Still no audit row on any path (ADR 0031 §5.2).
//
// MUTATION: the list taken away -> the link stays live, the request is "kept" -> red.
func TestPanelIdentityReset_ALateWithdrawalUnlocksThePerson(t *testing.T) {
	mail := &recordingChannel{err: errSendFailed}
	p, owner, ident, hook, logged := identityOutcomeHarness(t, mail)
	recoveryClocks(p.reset)
	hook.down.Store(true)

	p.askReset(t, p.email)
	waitUntil(t, 20*time.Second, "the lost withdrawal, owed", func() bool {
		return logged.count(lineIdentityLost) == 1 && logged.count("recovery=retrying") == 1
	})
	waitUntil(t, 20*time.Second, "a later try failing", func() bool { return logged.count(lineOwedAgain) >= 1 })
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Fatalf("CONTROL: %d live link(s) while the database is out of reach, want 1", n)
	}
	hook.down.Store(false)
	waitUntil(t, 20*time.Second, "the list's withdrawal", func() bool { return logged.count(lineOwedRecovered) == 1 })
	if n := liveIdentityLinks(t, owner, ident); n != 0 {
		t.Fatalf("%d live link(s) after the later try, want 0", n)
	}

	mail.mu.Lock()
	mail.err = nil
	mail.mu.Unlock()
	p.askReset(t, p.email)
	waitUntil(t, 20*time.Second, "a fresh link delivered", func() bool { return len(mail.all()) == 2 })
	if d := mail.all()[1]; !d.EveryBusiness {
		t.Error("the fresh link is not the identity's")
	}
	if logged.count("panel recovery: identity link kept") != 0 {
		t.Errorf("the request after the later try was kept:\n%s", logged)
	}
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Errorf("%d live link(s), want the fresh one", n)
	}
	for _, a := range []string{ActionAdminResetUndelivered, ActionAdminResetRequested, ActionAdminResetLimited} {
		if n := p.auditCount(t, a); n != 0 {
			t.Errorf("%d %s row(s) for a per-identity link, want 0", n, a)
		}
	}
	if strings.Contains(strings.ToLower(logged.String()), strings.ToLower(p.email)) {
		t.Error("a line carries the address")
	}
}

// TestPanelIdentityReset_ALateWithdrawalLeavesTheNewLinkAlone is T197's autocommit finding made
// deterministic: identity_withdraw_reset takes effect and the client sees an error, twice. The link is
// dead, the list still owes it, the person asks again and is delivered a fresh link; the list's try
// then withdraws the old link a second time, which moves nothing, and the fresh link lives.
//
// The identity twin of TestResetRecoveryDB_ALateWithdrawalKillsOnlyItsOwnLink, whose mutations are run
// there: identity_withdraw_reset is keyed by the link's own hash in the definer, so a wrong-target
// mutant here would be a migration, not a line.
func TestPanelIdentityReset_ALateWithdrawalLeavesTheNewLinkAlone(t *testing.T) {
	mail := &recordingChannel{err: errSendFailed}
	p, owner, ident, hook, logged := identityOutcomeHarness(t, mail)
	recoveryClocks(p.reset)
	p.reset.clocks.recoveryFirst = time.Hour
	hook.applied.Store(true)

	p.askReset(t, p.email)
	waitUntil(t, 20*time.Second, "the withdrawal, owed", func() bool { return logged.count("recovery=retrying") == 1 })
	if n := liveIdentityLinks(t, owner, ident); n != 0 {
		t.Fatalf("PREMISE: %d live link(s); the hook's withdrawals took effect, want 0", n)
	}
	hook.applied.Store(false)
	mail.mu.Lock()
	mail.err = nil
	mail.mu.Unlock()
	p.askReset(t, p.email)
	waitUntil(t, 20*time.Second, "the fresh link delivered", func() bool { return len(mail.all()) == 2 })

	dueNow(p.reset)
	waitUntil(t, 20*time.Second, "the list's try", func() bool { return logged.count(lineOwedRecovered) == 1 })
	if n := liveIdentityLinks(t, owner, ident); n != 1 {
		t.Errorf("%d live link(s), want the fresh one: the late withdrawal killed a link it was not handed", n)
	}
}

// TestResetRecoveryDB_AFullListClearsOnAHealthyDatabase measures the bound's cost: resetRecoverySize
// withdrawals owed, the real Withdraw on a healthy database, the shipped pass of resetRecoveryPass. The
// list clears in passes the worker takes between its other work, and nothing is lost. The grants are
// fakes inside a real business, so each UPDATE matches no row: the round trip and the transaction are
// paid, as in TestResetOutboxDB_AFullRetryQueueIsAccountedForAtTheDrain. The log line is the
// measurement; the bound only says it ends.
func TestResetRecoveryDB_AFullListClearsOnAHealthyDatabase(t *testing.T) {
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
			VALUES ($1, 'Owed List Ltd', $2, 'bar', 'single')`, tenantID, "VAT-"+tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	real, err := adminauth.NewResets(data, adminTestConfig())
	if err != nil {
		t.Fatalf("adminauth.NewResets: %v", err)
	}
	var logged outcomeLog
	resets := &realWithdrawals{fakeResets: &fakeResets{}, real: real}
	h, err := NewAdminReset(resets, refusingChannel{&recordingChannel{}}, trail, adminTestConfig(), slogOver(&logged))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	h.clocks.recoveryFirst = time.Hour
	grants := grantsFor("owed@list.example.test", resetRecoverySize)
	for i := range grants {
		grants[i].Issued.Reset.TenantID = tenantID
		if fate := h.owe(ctx, grants[i]); fate != recoveryRetrying {
			t.Fatalf("CONTROL: owed withdrawal %d refused: %s", i+1, fate)
		}
	}
	start := time.Now()
	dueNow(h)
	waitUntil(t, 60*time.Second, "the full list cleared", func() bool {
		return len(resets.withdrawals()) == resetRecoverySize && h.outbox.owedCount() == 0
	})
	took := time.Since(start)
	t.Logf("%d owed withdrawals, healthy database, pass %v: cleared in %v", resetRecoverySize, resetRecoveryPass, took)
	if n := logged.count(lineOwedRecovered); n != resetRecoverySize {
		t.Errorf("%d success line(s), want %d", n, resetRecoverySize)
	}
	if n := logged.count(lineWithdrawalLost) + logged.count(lineOwedAgain); n != 0 {
		t.Errorf("%d failure line(s) on a healthy database, want none", n)
	}
}
