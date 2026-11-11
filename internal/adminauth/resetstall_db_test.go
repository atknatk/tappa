package adminauth

// resetstall_db_test.go -- T219 against real Postgres: a lock held in another session stalls a
// request's mint, and ResetMintLockWait bounds how long. The locks are the issuers' own advisory keys
// (LockAdminForResetIssue's, identity_open_reset's), taken on a connection of the test's own, so
// nothing else in the database waits. Every holder releases on a timer too: a mutation that removes
// the bound waits for that timer, not forever.

import (
	"context"
	"os"
	"slices"
	"sync"
	"sync/atomic"
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

// stallCeiling is how long a stalled call may take: internal/handler's resetRequestFloor, written here
// as a number because this package cannot import that one. It does NOT follow ResetMintLockWait on
// purpose -- a ceiling derived from the constant would stay green while the constant grew past the
// floor (an audit raised it to 900 ms and every stall test stayed green);
// TestResetMintLockWait_FitsUnderTheFloor pins the constant itself.
const stallCeiling = 250 * time.Millisecond

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
// holder's 3 s and mints -> red. ResetMintLockWait raised to 900 ms -> over stallCeiling -> red. The stall treated as an error (the LockWaitExpired branch removed) ->
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
	if ceiling := stallCeiling; took > ceiling {
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
	holdIssuingLocks(t, 30*time.Second, keys...)

	r := cheapResets(t, d)
	r.mintLockWait = 500 * time.Millisecond
	// The fastest of three calls (liveLinkRounds' reason in internal/handler: one pause on a shared
	// machine is never a shape's fastest sample). The first call mints the newest account's link; the
	// later ones find it live and keep it, and the seven locked accounts stall every time.
	const rounds = 3
	took := make([]time.Duration, 0, rounds)
	for i := 0; i < rounds; i++ {
		start := time.Now()
		grants, kept, stalled, err := r.IssueForEmail(ctx, email)
		took = append(took, time.Since(start))
		if err != nil {
			t.Fatalf("round %d: IssueForEmail: %v", i+1, err)
		}
		if len(stalled) != ResetWindow-1 {
			t.Errorf("round %d: %d stalled, want %d", i+1, len(stalled), ResetWindow-1)
		}
		newest := accounts[ResetWindow-1]
		if i == 0 && (len(grants) != 1 || grants[0].Issued.Reset.AdminUserID != newest.admin || len(kept) != 0) {
			t.Fatalf("%d grant(s), want exactly the unlocked newest account's: a spent budget must not stall an account nobody locked", len(grants))
		}
		if i > 0 && (len(grants) != 0 || len(kept) != 1) {
			t.Fatalf("round %d: %d grant(s), %d kept; want the newest account's live link kept", i+1, len(grants), len(kept))
		}
	}
	fastest := slices.Min(took)
	t.Logf("%d of %d accounts locked: returned after %v, fastest %v (budget %v)", len(keys), ResetWindow, took, fastest, r.mintLockWait)
	if ceiling := 5 * r.mintLockWait / 2; fastest > ceiling {
		t.Errorf("the fastest call took %v, over %v: the accounts' waits did not share one budget", fastest, ceiling)
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
	if ceiling := stallCeiling; took > ceiling {
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

// TestIssueForEmail_TwoWaitsInOneMintShareTheBudget (T219's security review): the issuing lock is held
// until most of the budget is gone, and the business row stays locked, so the INSERT's foreign-key
// check waits too. Both waits come out of ONE budget: the bound is set again before the INSERT with
// what is left. The first version set it once per transaction, so the INSERT got the whole budget
// again (measured in the review: 302/296/299 ms against 150 ms).
//
// The budget is lengthened to 400 ms and the issuing lock released at 250 ms so the two shapes are far
// apart: about 330 ms now, about 650 ms with the bound set once. MUTATION: no bound before the INSERT
// -> about 650 ms -> red.
func TestIssueForEmail_TwoWaitsInOneMintShareTheBudget(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Two Waits Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Two Waits")
	r := cheapResets(t, d)
	r.mintLockWait = 400 * time.Millisecond

	// The business row, for the INSERT's foreign key, on the owner's connection (FOR UPDATE conflicts
	// with the key share the check takes), released at cleanup or by its timer.
	row, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatalf("connect the row holder: %v", err)
	}
	rowTx, err := row.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rowTx.Exec(ctx, `SELECT 1 FROM tenants WHERE id = $1 FOR UPDATE`, tenantID); err != nil {
		t.Fatalf("lock the business row: %v", err)
	}
	t.Cleanup(func() {
		if err := rowTx.Rollback(context.Background()); err != nil {
			t.Errorf("release the business row: %v", err)
		}
		if err := row.Close(context.Background()); err != nil {
			t.Errorf("close the row holder: %v", err)
		}
	})
	const issuingHeld = 250 * time.Millisecond
	holdIssuingLocks(t, issuingHeld, adminIssuingKey(admin))

	start := time.Now()
	grants, _, stalled, err := r.IssueForEmail(ctx, email)
	took := time.Since(start)
	t.Logf("issuing lock held %v, then the business row: returned after %v (budget %v)", issuingHeld, took, r.mintLockWait)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if len(grants) != 0 || len(stalled) != 1 {
		t.Fatalf("%d grant(s), %d stalled; want the account stalled at the INSERT", len(grants), len(stalled))
	}
	if took < issuingHeld {
		t.Fatalf("CONTROL: returned after %v, before the issuing lock went (%v): the INSERT never waited", took, issuingHeld)
	}
	if ceiling := r.mintLockWait + 60*time.Millisecond; took > ceiling {
		t.Errorf("the call took %v, over %v: the INSERT's wait was not bounded by what the issuing lock left", took, ceiling)
	}
	if ids := liveLinkIDs(t, d, tenantID, admin); len(ids) != 0 {
		t.Fatalf("%d live link(s), want 0", len(ids))
	}
}

// TestLockWaitFor_DividesTheRemainderAmongAStatementsWaits: a statement that can wait k times gets 1/k
// of what is left of the request's budget for each wait, so all k together cannot exceed it; past the
// budget each wait still gets a millisecond (the try is made, only a held lock stalls it).
//
// MUTATION: no division -> the two-wait case gets the whole remainder -> red.
func TestLockWaitFor_DividesTheRemainderAmongAStatementsWaits(t *testing.T) {
	r := &Resets{mintLockWait: 400 * time.Millisecond}
	ctx := r.withMintClock(context.Background())
	if got := r.lockWaitFor(ctx, 1); got > 400*time.Millisecond || got < 350*time.Millisecond {
		t.Errorf("one wait: %v, want about the whole 400 ms", got)
	}
	if got := r.lockWaitFor(ctx, 2); got > 200*time.Millisecond || got < 175*time.Millisecond {
		t.Errorf("two waits: %v each, want about half of 400 ms", got)
	}
	spent := context.WithValue(context.Background(), mintClockKey{}, time.Now().Add(-time.Second))
	if got := r.lockWaitFor(spent, 2); got != time.Millisecond {
		t.Errorf("a spent budget: %v, want 1ms", got)
	}
	if got := r.lockWaitFor(context.Background(), 2); got != 200*time.Millisecond {
		t.Errorf("no request clock: %v, want half of mintLockWait", got)
	}
}

// lockHolder is a session of the test's own that holds one lock in an open transaction: its backend
// pid, for releaseOnceWaitedOn, and its release (safe to call twice; also at cleanup, and after a
// minute in any case, so a mutation that waits cannot wait forever).
type lockHolder struct {
	pid     uint32
	release func()
}

// holdInOwnSession opens a session on dsn, begins, and runs query, which must take its lock and
// answer one row (an advisory lock taken by SELECT, or SELECT ... FOR UPDATE of one row -- which
// conflicts with the key share a foreign-key check takes).
func holdInOwnSession(t *testing.T, dsn, query string, args ...any) lockHolder {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect the lock holder: %v", err)
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("take the lock (%s): %v, %d row(s)", query, err, tag.RowsAffected())
	}
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		select {
		case <-done:
		case <-time.After(time.Minute):
		}
		if err := tx.Rollback(context.Background()); err != nil {
			t.Errorf("release the lock: %v", err)
		}
		if err := c.Close(context.Background()); err != nil {
			t.Errorf("close the lock holder: %v", err)
		}
	}()
	var once sync.Once
	release := func() {
		once.Do(func() { close(done) })
		<-ended
	}
	t.Cleanup(release)
	return lockHolder{pid: c.PgConn().PID(), release: release}
}

// releaseOnceWaitedOn watches, from an owner session, for a backend blocked by first
// (pg_blocking_pids), and `after` the moment it sees one releases every holder; waited reports
// whether it saw one. Counting from the WAIT, not from the call, is what keeps the tests below off
// the clock: whatever the call spent before the statement, the statement's first wait is held
// exactly `after` past its start.
func releaseOnceWaitedOn(t *testing.T, after time.Duration, first lockHolder, all ...lockHolder) (waited func() bool) {
	t.Helper()
	ctx := context.Background()
	w, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatalf("connect the watcher: %v", err)
	}
	var saw atomic.Bool
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		defer func() {
			if err := w.Close(context.Background()); err != nil {
				t.Errorf("close the watcher: %v", err)
			}
		}()
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
			var blocked bool
			if err := w.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity a
			                            WHERE $1 = ANY (pg_catalog.pg_blocking_pids(a.pid)))`, int64(first.pid)).Scan(&blocked); err != nil {
				t.Errorf("watch for a waiter: %v", err)
				return
			}
			if blocked {
				saw.Store(true)
				time.Sleep(after)
				for _, h := range all {
					h.release()
				}
				return
			}
		}
	}()
	t.Cleanup(func() { <-ended })
	return saw.Load
}

// stallSplitBudget and stallSplitAfter shape the two tests below (T236 (d)): an 800 ms budget, and
// a statement whose FIRST wait is held 640 ms past its start -- over half of any remainder, under
// the whole one unless the call spent more than 160 ms before the statement -- after which both of
// its locks go. With the statement's waits divided by 2, the first wait runs out at 400 ms at the
// latest and the account stalls: nothing on that side depends on how loaded the machine is but the
// 240 ms between the timeout and the release. Divided by 1, the first wait is granted, the second
// lock is already gone, and the link is minted.
//
// Why not a ceiling on the call's duration, like the tests above: measured while writing these, at
// load ~300 locally, the unmutated cases took 440-550 ms against an 860 ms ceiling and once went over.
const (
	stallSplitBudget = 800 * time.Millisecond
	stallSplitAfter  = 640 * time.Millisecond
)

// TestIssueForEmail_TheInsertsTwoWaitsSplitWhatIsLeft (T236 (d)): CreatePasswordReset's INSERT can
// wait twice, once per foreign key (tenants, then admin_users -- the check triggers fire in name
// order, read from the catalog below), so createResetWaits divides what is left by 2. Both rows are
// held; the business row's wait is held stallSplitAfter; the account must stall with nothing minted.
// TestIssueForEmail_TwoWaitsInOneMintShareTheBudget cannot see the divisor: there the INSERT waits once.
//
// MUTATION (measured): createResetWaits = 1 -> the link is minted -> red.
func TestIssueForEmail_TheInsertsTwoWaitsSplitWhatIsLeft(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Split Waits Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Split Waits")
	r := cheapResets(t, d)
	r.mintLockWait = stallSplitBudget

	cat, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect for the catalog read: %v", err)
	}
	defer func() {
		if err := cat.Close(ctx); err != nil {
			t.Errorf("close the catalog connection: %v", err)
		}
	}()
	var order []string
	rows, err := cat.Query(ctx, `SELECT c.confrelid::regclass::text FROM pg_catalog.pg_trigger t
	                               JOIN pg_catalog.pg_constraint c ON c.oid = t.tgconstraint
	                              WHERE t.tgrelid = 'public.password_resets'::regclass AND c.contype = 'f'
	                                AND t.tgfoid = 'pg_catalog."RI_FKey_check_ins"'::regproc
	                              ORDER BY t.tgname`)
	if err == nil {
		order, err = pgx.CollectRows(rows, pgx.RowTo[string])
	}
	if err != nil {
		t.Fatalf("read password_resets' foreign-key checks: %v", err)
	}
	if !slices.Equal(order, []string{"tenants", "admin_users"}) {
		t.Fatalf("PREMISE: password_resets' foreign-key checks fire in the order %v, want [tenants admin_users]", order)
	}
	owner := os.Getenv("DATABASE_MIGRATE_URL")
	business := holdInOwnSession(t, owner, `SELECT 1 FROM tenants WHERE id = $1 FOR UPDATE`, tenantID)
	account := holdInOwnSession(t, owner, `SELECT 1 FROM admin_users WHERE id = $1 FOR UPDATE`, admin)
	waited := releaseOnceWaitedOn(t, stallSplitAfter, business, business, account)

	start := time.Now()
	grants, _, stalled, err := r.IssueForEmail(ctx, email)
	t.Logf("both rows held, the business row's wait %v: returned after %v (budget %v)", stallSplitAfter, time.Since(start), r.mintLockWait)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if !waited() {
		t.Fatal("CONTROL: no backend ever waited on the business row: the INSERT's first wait did not happen")
	}
	if len(grants) != 0 || len(stalled) != 1 {
		t.Fatalf("%d grant(s), %d stalled; want the account stalled at the INSERT's first wait -- a grant means "+
			"that wait was given the whole remainder, and its second could have been too", len(grants), len(stalled))
	}
	if ids := liveLinkIDs(t, d, tenantID, admin); len(ids) != 0 {
		t.Fatalf("%d live link(s), want 0", len(ids))
	}
}

// TestIssueForPerson_TheOpeningsTwoWaitsSplitWhatIsLeft (T236 (d)), flag on: identity_open_reset can
// wait twice -- its advisory lock, then admin_identity_resets' foreign key on the identity row -- so
// openIdentityResetWaits divides what is left by 2. Both are held; the opening lock's wait is held
// stallSplitAfter; the identity must stall with nothing opened.
// TestIssueForPerson_AHeldIdentityLockStallsOnlyTheIdentity cannot see the divisor: there it waits once.
//
// MUTATION (measured): openIdentityResetWaits = 1 -> the identity's link is opened -> red.
func TestIssueForPerson_TheOpeningsTwoWaitsSplitWhatIsLeft(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)
	r := cheapResets(t, d)
	r.mintLockWait = stallSplitBudget

	opening := holdInOwnSession(t, os.Getenv("DATABASE_URL"), `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, identityOpeningKey(w.ident))
	row := holdInOwnSession(t, os.Getenv("DATABASE_MIGRATE_URL"), `SELECT 1 FROM admin_identities WHERE id = $1 FOR UPDATE`, w.ident)
	waited := releaseOnceWaitedOn(t, stallSplitAfter, opening, opening, row)

	start := time.Now()
	p, err := r.IssueForPerson(ctx, w.typed)
	t.Logf("opening lock and identity row held, the opening's wait %v: returned after %v (budget %v)", stallSplitAfter, time.Since(start), r.mintLockWait)
	if err != nil {
		t.Fatalf("IssueForPerson: %v", err)
	}
	if !waited() {
		t.Fatal("CONTROL: no backend ever waited on the opening lock: the opening's first wait did not happen")
	}
	if len(p.Stalled) != 1 || p.Stalled[0].IdentityID != w.ident || p.IdentityKept != nil {
		t.Fatalf("stalled %+v, identity kept %v; want exactly the identity stalled -- an opened link means the "+
			"opening's first wait was given the whole remainder, and its second could have been too", p.Stalled, p.IdentityKept)
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 0 {
		t.Fatalf("%d identity link(s) after a stalled opening, want 0", n)
	}
}
