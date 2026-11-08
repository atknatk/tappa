package adminauth

// resetstall_db_test.go -- T219 against real Postgres: a lock held in another session stalls a
// request's mint, and ResetMintLockWait bounds how long. The locks are the issuers' own advisory keys
// (LockAdminForResetIssue's, identity_open_reset's), taken on a connection of the test's own, so
// nothing else in the database waits. Every holder releases on a timer too: a mutation that removes
// the bound waits for that timer, not forever.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// holdIssuingLocks takes the issuing lock of every key on a connection of its own -- the shape of a
// concurrent request that holds it -- and releases them all after hold, or at release().
func holdIssuingLocks(t *testing.T, hold time.Duration, keys ...string) (release func()) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect the lock holder: %v", err)
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the lock holder: %v", err)
	}
	for _, k := range keys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, k); err != nil {
			t.Fatalf("take the lock %s: %v", k, err)
		}
	}
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		select {
		case <-done:
		case <-time.After(hold):
		}
		if err := tx.Rollback(context.Background()); err != nil {
			t.Errorf("release the locks: %v", err)
		}
		if err := c.Close(context.Background()); err != nil {
			t.Errorf("close the lock holder: %v", err)
		}
	}()
	var released bool
	release = func() {
		if !released {
			released = true
			close(done)
		}
		<-ended
	}
	t.Cleanup(release)
	return release
}

// The issuers' advisory keys: LockAdminForResetIssue's and identity_open_reset's (00072).
func adminIssuingKey(admin uuid.UUID) string { return "admin-reset:" + admin.String() }
func identityOpeningKey(id uuid.UUID) string { return "admin-identity-reset:" + id.String() }

// TestIssueForEmail_AHeldLockStallsTheMintAndLeavesNothing: another session holds the administrator's
// issuing lock. The request gives up after ResetMintLockWait instead of waiting for it, returns the
// account stalled with a nil error (so the caller answers what it answers for an unknown address), and
// leaves no link behind -- the transaction was rolled back at the wait. Once the lock goes, the next
// request mints. Before T219 the same setup waited for the lock (measured over HTTP: the request's
// deadline, then a 500).
//
// MUTATIONS: no lock_timeout in the mint transaction (BoundLockWaits removed) -> the call waits for the
// holder's 3 s and mints -> red. The stall treated as an error (the LockWaitExpired branch removed) ->
// a non-nil error -> red.
func TestIssueForEmail_AHeldLockStallsTheMintAndLeavesNothing(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Stalled Mint Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Stalled")
	r := cheapResets(t, d)

	release := holdIssuingLocks(t, 3*time.Second, adminIssuingKey(admin))
	start := time.Now()
	grants, kept, stalled, err := r.IssueForEmail(ctx, email)
	took := time.Since(start)
	t.Logf("a held issuing lock: the call returned after %v (budget %v)", took, ResetMintLockWait)
	if err != nil {
		t.Fatalf("IssueForEmail: %v -- a stall must not be an error: the caller would answer a registered address differently", err)
	}
	if len(grants) != 0 || len(kept) != 0 {
		t.Fatalf("%d grant(s), %d kept while the lock was held, want none", len(grants), len(kept))
	}
	if len(stalled) != 1 || stalled[0].TenantID != tenantID || stalled[0].AdminUserID != admin || stalled[0].IdentityID != uuid.Nil {
		t.Fatalf("stalled %+v, want exactly this account", stalled)
	}
	if ceiling := time.Second; took > ceiling {
		t.Errorf("the call took %v, over %v: the mint waited for the lock instead of giving up", took, ceiling)
	}
	if ids := liveLinkIDs(t, d, tenantID, admin); len(ids) != 0 {
		t.Fatalf("%d live link(s) after a stalled mint, want 0: the owner would be answered kept with a link no inbox holds", len(ids))
	}
	release()

	grants, _, stalled, err = r.IssueForEmail(ctx, email)
	if err != nil || len(grants) != 1 || len(stalled) != 0 {
		t.Fatalf("after the lock went: %v, %d grant(s), %d stalled; want one fresh link", err, len(grants), len(stalled))
	}
}

// TestIssueForEmail_OneWaitBudgetForTheWholeWindow: a full ResetWindow behind one address, the issuing
// locks of all but the newest account held elsewhere. The waits share ONE budget, so the call returns
// in about one budget, not one per account; and the newest account, resolved last -- after the budget
// is spent -- still gets its link, because a mint past the budget is still tried (with a millisecond to
// wait): only a held lock stalls an account.
//
// The budget is lengthened to 500 ms so one budget and seven are far apart on a loaded machine.
// MUTATIONS: the budget per transaction instead of per request (withMintClock not applied) -> about
// 3.5 s -> red. No try past the budget (the account skipped) -> the newest account stalled -> red.
func TestIssueForEmail_OneWaitBudgetForTheWholeWindow(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	email := randEmail(t)
	type account struct{ tenant, admin uuid.UUID }
	accounts := make([]account, ResetWindow)
	for i := range accounts {
		tenant := newTenantRow(t, d, "Window Stall Ltd")
		accounts[i] = account{tenant, newAdminRow(t, d, tenant, email, "p", "active", "owner", "Window")}
	}
	keys := make([]string, 0, ResetWindow-1)
	for _, a := range accounts[:ResetWindow-1] {
		keys = append(keys, adminIssuingKey(a.admin))
	}
	holdIssuingLocks(t, 10*time.Second, keys...)

	r := cheapResets(t, d)
	r.mintLockWait = 500 * time.Millisecond
	start := time.Now()
	grants, kept, stalled, err := r.IssueForEmail(ctx, email)
	took := time.Since(start)
	t.Logf("%d of %d accounts locked: returned after %v (budget %v)", len(keys), ResetWindow, took, r.mintLockWait)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if ceiling := 5 * r.mintLockWait / 2; took > ceiling {
		t.Errorf("the call took %v, over %v: the accounts' waits did not share one budget", took, ceiling)
	}
	if len(stalled) != ResetWindow-1 || len(kept) != 0 {
		t.Errorf("%d stalled, %d kept, want %d stalled", len(stalled), len(kept), ResetWindow-1)
	}
	newest := accounts[ResetWindow-1]
	if len(grants) != 1 || grants[0].Issued.Reset.AdminUserID != newest.admin {
		t.Fatalf("%d grant(s), want exactly the unlocked newest account's: a spent budget must not stall an account nobody locked", len(grants))
	}
}

// TestIssueForPerson_AHeldIdentityLockStallsOnlyTheIdentity: flag on (the per-identity request).
// Another session holds the identity's opening lock. The identity is returned stalled, nothing is
// opened for it, and the unlinked row's per-row link -- minted before -- stays good and is returned:
// the stall is not a failure, so nothing is withdrawn. Before T219 the opening waited for the lock
// (over HTTP: the request deadline, then a 500).
//
// MUTATIONS: no lock_timeout in OpenIdentityReset -> the call waits for the holder's 3 s and opens ->
// red. The stall treated as an error -> a non-nil error and the row's link withdrawn -> red.
func TestIssueForPerson_AHeldIdentityLockStallsOnlyTheIdentity(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)
	r := cheapResets(t, d)

	release := holdIssuingLocks(t, 3*time.Second, identityOpeningKey(w.ident))
	start := time.Now()
	p, err := r.IssueForPerson(ctx, w.typed)
	took := time.Since(start)
	t.Logf("a held identity lock: the call returned after %v (budget %v)", took, ResetMintLockWait)
	if err != nil {
		t.Fatalf("IssueForPerson: %v -- a stall must not be an error", err)
	}
	if len(p.Stalled) != 1 || p.Stalled[0].IdentityID != w.ident || p.IdentityKept != nil {
		t.Fatalf("stalled %+v, identity kept %v; want exactly the identity stalled", p.Stalled, p.IdentityKept)
	}
	if ceiling := time.Second; took > ceiling {
		t.Errorf("the call took %v, over %v: the opening waited for the lock instead of giving up", took, ceiling)
	}
	if len(p.Grants) != 1 || p.Grants[0].IdentityID != uuid.Nil || p.Grants[0].Issued.Reset.AdminUserID != w.rowB {
		t.Fatalf("%d grant(s), want the unlinked row's link only", len(p.Grants))
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 0 {
		t.Fatalf("%d identity link(s) after a stalled opening, want 0", n)
	}
	if n := w.int(`SELECT count(*) FROM password_resets WHERE admin_user_id = $1 AND cancelled_at IS NULL`, w.rowB); n != 1 {
		t.Errorf("%d live link(s) for the unlinked row, want 1: a stall must not withdraw what was minted before it", n)
	}
	if p.Empty() {
		t.Error("a stalled request reports Empty: the caller would log it as an unknown address")
	}
	release()

	p, err = r.IssueForPerson(ctx, w.typed)
	if err != nil || len(p.Stalled) != 0 || len(p.Grants) != 1 || p.Grants[0].IdentityID != w.ident {
		t.Fatalf("after the lock went: %v, %+v; want the identity's link opened", err, p)
	}
}
