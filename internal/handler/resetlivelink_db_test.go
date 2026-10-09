package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
	"github.com/atknatk/tappa/internal/store"
)

// TestPanelRecoveryDB_OneLiveLinkPerAccount is M10 EM-7C's rule in the WIRED flow,
// over real HTTP against real Postgres and the real resolver (the panel harness's
// recovery form, its channel a recorder):
//
//  1. a request whose send FAILS records the link undelivered AND withdraws it, so
//  2. the next request mints a fresh link at once and it is delivered — the failed
//     send did not hold the account's one slot;
//  3. two more requests while that link lives mint nothing and send nothing: each
//     writes one admin.recovery.kept row naming the live link, in this tenant, and
//     the page is the page;
//  4. at the end exactly one link of this administrator is live — the delivered one.
func TestPanelRecoveryDB_OneLiveLinkPerAccount(t *testing.T) {
	p := newPanelHarness(t)
	ask := func(t *testing.T) string {
		t.Helper()
		res, body := p.get(t, adminResetPath)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d", adminResetPath, res.StatusCode)
		}
		res, body = p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, body)}, "email": {p.email}})
		if res.StatusCode != http.StatusOK {
			t.Fatalf("POST %s = %d", adminResetPath, res.StatusCode)
		}
		return body
	}
	setSendErr := func(err error) {
		p.mail.mu.Lock()
		p.mail.err = err
		p.mail.mu.Unlock()
	}

	// 1. The send fails.
	setSendErr(errors.New("the relay refused the message"))
	first := ask(t)
	waitUntil(t, 10*time.Second, "the failed link's undelivered row", func() bool {
		return p.auditCount(t, ActionAdminResetUndelivered) == 1
	})
	if live := p.liveResetIDs(t); len(live) != 0 {
		t.Fatalf("%d live link(s) after an undelivered send, want none: the undelivered link still "+
			"holds the account's one slot and every request for the next hour would mint nothing", len(live))
	}

	// 2. The next request mints and delivers.
	setSendErr(nil)
	second := ask(t)
	waitUntil(t, 10*time.Second, "the fresh link's requested row", func() bool {
		return p.auditCount(t, ActionAdminResetRequested) == 1
	})
	delivered := p.mail.all()
	if len(delivered) != 2 || delivered[1].Recipient != p.email {
		t.Fatalf("the channel was handed %d link(s), want the failed one and a fresh one to the row's address", len(delivered))
	}
	fresh := delivered[1].ResetID
	if fresh == delivered[0].ResetID {
		t.Fatal("the fresh link is the withdrawn one")
	}

	// 3. Two more requests while it lives: nothing minted, nothing sent, two kept rows.
	third, fourth := ask(t), ask(t)
	if n := p.auditCount(t, ActionAdminResetKept); n != 2 {
		t.Fatalf("%d kept row(s) when the requests answered, want 2 — written by the request itself", n)
	}
	p.drainReset(t)
	if n := len(p.mail.all()); n != 2 {
		t.Errorf("the channel was handed %d link(s), want still 2: a live link must keep a request from minting", n)
	}
	for _, d := range p.keptDetails(t) {
		if !strings.Contains(d, `"reset_id": "`+fresh.String()+`"`) || !strings.Contains(d, `"outcome": "kept"`) || strings.Contains(d, "@") {
			t.Errorf("a kept row's detail is %s, want outcome kept naming the live link %s and no address", d, fresh)
		}
	}
	for i, b := range []string{second, third, fourth} {
		if b != first {
			t.Errorf("answer %d differs from the first: the page told the requester something", i+2)
		}
	}

	// 4. Exactly the delivered link is live.
	if live := p.liveResetIDs(t); len(live) != 1 || live[0] != fresh {
		t.Errorf("live links %v, want exactly the delivered %v", live, fresh)
	}
}

// liveResetIDs reads this harness's administrator's live recovery links through the
// production read, in the tenant's own context.
func (p *panelHarness) liveResetIDs(t *testing.T) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := store.New(tx).ListLivePasswordResetsForAdmin(ctx, store.ListLivePasswordResetsForAdminParams{
			TenantID: p.tenantID, AdminUserID: p.adminID,
		})
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return e
	})
	if err != nil {
		t.Fatalf("ListLivePasswordResetsForAdmin: %v", err)
	}
	return ids
}

// keptDetails reads the detail of every kept row of this harness's administrator.
func (p *panelHarness) keptDetails(t *testing.T) []string {
	t.Helper()
	var out []string
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, ActionAdminResetKept, p.adminID.String())
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if e := rows.Scan(&d); e != nil {
				return e
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read kept rows: %v", err)
	}
	return out
}

// refusingChannel is a recordingChannel whose breaker refuses: every grant takes
// dispatch's fallback path on the request itself.
type refusingChannel struct{ *recordingChannel }

func (refusingChannel) RefusingResets() bool { return true }

// TestResetOutboxDB_TheFallbackWithdrawalsStayUnderTheFloor measures what M10 EM-7C
// added to the REQUEST path: when the breaker refuses (and likewise when the outbox is
// full or closing), dispatch records each grant undelivered on the request itself —
// and since EM-7C withdraws its link first, so a registered address pays, per grant,
// one withdrawal transaction and one audit INSERT before the floor ends. A FULL window
// (adminauth.ResetWindow grants, the most one request mints) against real Postgres —
// the real adminauth.Resets.Withdraw and the real audit recorder — must still answer
// inside [resetRequestFloor, resetRequestFloor+50ms], three times. The grants are the
// fake's, so each withdrawal's UPDATE matches no row: the round trip and the
// transaction are paid, the one-row write is not.
func TestResetOutboxDB_TheFallbackWithdrawalsStayUnderTheFloor(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). Run `make test`.")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: withSmallPool(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	tenantID := uuid.New()
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Fallback Withdrawal Ltd', $2, 'bar', 'single')`, tenantID, "VAT-"+tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	const known = "window@fallback.example.test"
	grants := grantsFor(known, adminauth.ResetWindow)
	for i := range grants {
		grants[i].Issued.Reset.TenantID = tenantID
	}
	real, err := adminauth.NewResets(data, adminTestConfig())
	if err != nil {
		t.Fatalf("adminauth.NewResets: %v", err)
	}
	resets := &realWithdrawals{fakeResets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: grants}}, real: real}
	ch := refusingChannel{&recordingChannel{}}
	h, err := NewAdminReset(resets, ch, trail, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	router := mountReset(h)

	const runs = 3
	for i := 0; i < runs; i++ {
		b := newBrowser(t, router)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
		start := time.Now()
		rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {known}})
		took := time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("run %d answered %d", i+1, rec.Code)
		}
		if took < resetRequestFloor || took > resetRequestFloor+50*time.Millisecond {
			t.Errorf("run %d answered in %v, outside [floor, floor+50ms] (floor %v): the fallback's %d withdrawals "+
				"and rows are visible in the clock", i+1, took, resetRequestFloor, len(grants))
		}
		t.Logf("run %d: %d grants refused by the breaker, each withdrawn and recorded, answered in %v", i+1, len(grants), took)
	}
	if w := resets.withdrawals(); len(w) != runs*len(grants) {
		t.Errorf("%d withdrawal(s) reached Postgres, want %d", len(w), runs*len(grants))
	}
	if n := len(ch.all()); n != 0 {
		t.Errorf("%d link(s) sent while the breaker refused, want none", n)
	}
	var rows int
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			tenantID, ActionAdminResetUndelivered).Scan(&rows)
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != runs*len(grants) {
		t.Errorf("%d undelivered row(s), want %d", rows, runs*len(grants))
	}
}
