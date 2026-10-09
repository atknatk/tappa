package adminauth

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// One live recovery link per account (M10 EM-7C, ADR 0022's EM-7C note), against REAL
// Postgres: the rule's state is password_resets itself, judged on the database's clock
// at the instant the read runs, so a fake would measure only its author's belief.

// liveLinkIDs lists an administrator's live links, newest first, through the
// production read.
func liveLinkIDs(t *testing.T, d *db.DB, tenantID, adminID uuid.UUID) []uuid.UUID {
	t.Helper()
	var rows []store.ListLivePasswordResetsForAdminRow
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListLivePasswordResetsForAdmin(ctx, store.ListLivePasswordResetsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID,
		})
		return e
	})
	if err != nil {
		t.Fatalf("ListLivePasswordResetsForAdmin: %v", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// grantFor returns the grant minted for adminID, or fails.
func grantFor(t *testing.T, grants []ResetGrant, adminID uuid.UUID) ResetGrant {
	t.Helper()
	for _, g := range grants {
		if g.Issued.Reset.AdminUserID == adminID {
			return g
		}
	}
	t.Fatalf("no link was minted for administrator %v (grants: %d)", adminID, len(grants))
	return ResetGrant{}
}

func mintedFor(grants []ResetGrant, adminID uuid.UUID) bool {
	for _, g := range grants {
		if g.Issued.Reset.AdminUserID == adminID {
			return true
		}
	}
	return false
}

// plusTagged is the same mailbox under a "+tag" — a DIFFERENT address to the resolver
// (citext compares the whole string), the same inbox to most providers.
func plusTagged(addr, tag string) string {
	return strings.Replace(addr, "@", "+"+tag+"@", 1)
}

// TestIssueForEmail_NeverLeavesAWindowWithoutALiveLinkOrAWayToGetOne is the security
// audit's scenario (a) against EM-7C's second round, on an injected clock: whoever
// asks, and however often, the owner of an account is never in a state where they
// hold no live link AND a request for their address would mint nothing.
//
// THE ATTACKER DOES EVERYTHING OPEN SIGNUP AND THE PUBLIC FORM ALLOW: he registers a
// business whose administrator's address is the owner's mailbox under a "+tag", has a
// link minted to it FIRST (so it is live for the whole hour), then asks for the
// owner's address 25 times — more than the 20 a day a per-mailbox counter allowed —
// across the owner's link's last seconds.
//
// THE CLOCK IS THE MINTING CLOCK (Resets.now): the owner's link is minted as if at
// the start of its hour, ResetTTL - 1.5 s ago, so it dies 1.5 s into the test and the
// test can watch the moment it does. Liveness itself stays the database's clock at the
// read (statement_timestamp()), the instant the consume statement's now() also means —
// the rule is decided on the clock that decides whether the link can be spent.
func TestIssueForEmail_NeverLeavesAWindowWithoutALiveLinkOrAWayToGetOne(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	owner := randEmail(t)
	ownerTenant := newTenantRow(t, d, "Window Owner Ltd")
	ownerAdmin := newAdminRow(t, d, ownerTenant, owner, "p", "active", "owner", "Owner")
	seeded := plusTagged(owner, "x")
	seededTenant := newTenantRow(t, d, "Window Seeded Ltd")
	seededAdmin := newAdminRow(t, d, seededTenant, seeded, "p", "active", "owner", "Seeded")

	attacker := cheapResets(t, d) // shipped ResetTTL, real clock
	late := cheapResets(t, d)
	const left = 1500 * time.Millisecond
	late.now = func() time.Time { return time.Now().Add(-(ResetTTL - left)) }

	// 0. The seeded account's own link, live for the whole hour.
	g0, _, err := attacker.IssueForEmail(ctx, seeded)
	if err != nil || !mintedFor(g0, seededAdmin) {
		t.Fatalf("seeding: %v, %d grant(s)", err, len(g0))
	}

	// 1. The owner asks and gets a link, although another account's link is live in
	//    the same mailbox. A rule keyed by the MAILBOX refuses here — and the owner
	//    then holds nothing for an hour and cannot ask.
	g1, _, err := late.IssueForEmail(ctx, owner)
	if err != nil {
		t.Fatalf("owner's request: %v", err)
	}
	if !mintedFor(g1, ownerAdmin) {
		t.Fatal("the owner holds no live link and their own request minted none, because " +
			"another account's link is live in the same mailbox: the window this test " +
			"exists to forbid (no live link AND no way to get one)")
	}
	l1 := grantFor(t, g1, ownerAdmin)
	if l1.Recipient != owner {
		t.Fatalf("the owner's link went to %q, not to the address on their row", l1.Recipient)
	}

	// 2. While it lives, nobody's request touches it.
	for i := 0; i < 25; i++ {
		g, kept, err := attacker.IssueForEmail(ctx, owner)
		if err != nil {
			t.Fatalf("attacker request %d: %v", i+1, err)
		}
		if len(g) != 0 {
			t.Fatalf("attacker request %d minted %d link(s) while the owner's was live; that "+
				"mint retires the owner's link (harm (a))", i+1, len(g))
		}
		if len(kept) != 1 || kept[0].ID != l1.Issued.Reset.ID {
			t.Fatalf("attacker request %d kept %+v, want the owner's live link", i+1, kept)
		}
		if ids := liveLinkIDs(t, d, ownerTenant, ownerAdmin); len(ids) != 1 || ids[0] != l1.Issued.Reset.ID {
			t.Fatalf("after attacker request %d the owner's live links are %v, want exactly %v",
				i+1, ids, l1.Issued.Reset.ID)
		}
	}
	if g, _, err := attacker.IssueForEmail(ctx, seeded); err != nil || len(g) != 0 {
		t.Fatalf("a second request for the seeded address: %v, %d grant(s) -- its own live "+
			"link must keep it too", err, len(g))
	}

	// 3. The owner's link dies on its own clock.
	deadline := time.Now().Add(left + 5*time.Second)
	for len(liveLinkIDs(t, d, ownerTenant, ownerAdmin)) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the owner's link never expired; the injected clock did not reach the row")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 4. The very next request — the attacker's — mints a fresh link for the OWNER,
	//    to the address on the owner's row: the owner holds a valid link without
	//    having asked, and the attacker cannot keep the account linkless.
	g4, _, err := attacker.IssueForEmail(ctx, owner)
	if err != nil {
		t.Fatalf("the first request after the expiry: %v", err)
	}
	if !mintedFor(g4, ownerAdmin) {
		t.Fatal("the owner's link has expired and the next request minted nothing for them: " +
			"a window with no live link and no way to get one")
	}
	l2 := grantFor(t, g4, ownerAdmin)
	if l2.Recipient != owner {
		t.Fatalf("the fresh link went to %q, not to the address on the owner's row", l2.Recipient)
	}
	if l2.Issued.Reset.RetiredCount != 0 {
		t.Errorf("the fresh link retired %d link(s); the expired one was not live", l2.Issued.Reset.RetiredCount)
	}

	// 5. The owner's own request now keeps that link, and the link works.
	g5, kept, err := late.IssueForEmail(ctx, owner)
	if err != nil || len(g5) != 0 || len(kept) != 1 || kept[0].ID != l2.Issued.Reset.ID {
		t.Fatalf("the owner's request after the fresh link: %v, %d grant(s), kept %+v", err, len(g5), kept)
	}
	if _, _, err := attacker.Consume(ctx, l2.Issued.Token, "the-owners-new-password"); err != nil {
		t.Fatalf("the owner could not spend the link the attacker's request produced: %v", err)
	}
	// The seeded account's link was never touched by any of this.
	if ids := liveLinkIDs(t, d, seededTenant, seededAdmin); len(ids) != 1 {
		t.Errorf("the seeded account holds %d live link(s), want its one", len(ids))
	}
}

// TestIssueForEmail_AnotherAccountInTheSameMailboxChangesNothing: accounts that fold
// into the owner's mailbox — a "+tag" address and the same address in another
// business (open signup allows both) — each hold their OWN link, and nothing done to
// them reaches the owner's: not a mint, not a kept link, not a refusal.
func TestIssueForEmail_AnotherAccountInTheSameMailboxChangesNothing(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	owner := randEmail(t)
	ownerTenant := newTenantRow(t, d, "Mailbox Owner Ltd")
	ownerAdmin := newAdminRow(t, d, ownerTenant, owner, "p", "active", "owner", "Owner")

	// The owner's live link first.
	g, _, err := r.IssueForEmail(ctx, owner)
	if err != nil || len(g) != 1 {
		t.Fatalf("owner: %v, %d grant(s)", err, len(g))
	}
	l1 := g[0].Issued.Reset.ID

	// A "+tag" account in another business: its requests mint for it alone.
	tagTenant := newTenantRow(t, d, "Mailbox Tag Ltd")
	tagAdmin := newAdminRow(t, d, tagTenant, plusTagged(owner, "seed"), "p", "active", "owner", "Tag")
	for i := 0; i < 3; i++ {
		g, kept, err := r.IssueForEmail(ctx, plusTagged(owner, "seed"))
		if err != nil {
			t.Fatalf("tag request %d: %v", i+1, err)
		}
		for _, k := range kept {
			if k.AdminUserID == ownerAdmin {
				t.Fatal("a request for the +tag address reported the owner's link")
			}
		}
		if i == 0 && !mintedFor(g, tagAdmin) {
			t.Fatal("the +tag account got no link of its own")
		}
		if mintedFor(g, ownerAdmin) {
			t.Fatal("a request for the +tag address minted for the owner")
		}
	}

	// The SAME address in another business, created after the owner's link: the next
	// request for the owner's address mints for it and keeps the owner's.
	sameTenant := newTenantRow(t, d, "Mailbox Same Ltd")
	sameAdmin := newAdminRow(t, d, sameTenant, strings.ToUpper(owner), "p", "active", "owner", "Same")
	g, kept, err := r.IssueForEmail(ctx, owner)
	if err != nil {
		t.Fatalf("owner again: %v", err)
	}
	if !mintedFor(g, sameAdmin) || mintedFor(g, ownerAdmin) {
		t.Fatalf("grants %d: want one for the new account and none for the owner", len(g))
	}
	if len(kept) != 1 || kept[0].ID != l1 {
		t.Fatalf("kept %+v, want only the owner's live link", kept)
	}
	if ids := liveLinkIDs(t, d, ownerTenant, ownerAdmin); len(ids) != 1 || ids[0] != l1 {
		t.Fatalf("the owner's live links are %v, want exactly their first one", ids)
	}
}

// TestIssueForEmail_MintsAgainOnceTheLinkCannotBeSpent: the rule's state is the link
// itself, so every way a link stops being spendable — expiry, consumption, withdrawal
// — makes the next request mint at once, with nothing to retire.
func TestIssueForEmail_MintsAgainOnceTheLinkCannotBeSpent(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	tenantID := newTenantRow(t, d, "Recovery Again Ltd")

	cases := []struct {
		name string
		kill func(t *testing.T, g ResetGrant)
	}{
		{"expired", nil},
		{"spent", func(t *testing.T, g ResetGrant) {
			if _, _, err := r.Consume(ctx, g.Issued.Token, "spent-password"); err != nil {
				t.Fatalf("Consume: %v", err)
			}
		}},
		{"withdrawn", func(t *testing.T, g ResetGrant) {
			if err := r.Withdraw(ctx, g.Issued.Reset.TenantID, g.Issued.Reset.ID); err != nil {
				t.Fatalf("Withdraw: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := randEmail(t)
			admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Again")
			issuer := r
			if tc.kill == nil {
				dead := cheapResets(t, d)
				dead.ttl = -time.Hour // already expired; no sleeping
				issuer = dead
			}
			g, _, err := issuer.IssueForEmail(ctx, email)
			if err != nil || len(g) != 1 {
				t.Fatalf("first: %v, %d grant(s)", err, len(g))
			}
			if tc.kill != nil {
				// CONTROL: while it lives, it is kept.
				if again, kept, err := r.IssueForEmail(ctx, email); err != nil || len(again) != 0 || len(kept) != 1 {
					t.Fatalf("CONTROL FAILED: a live link was not kept: %v, %d grant(s), %d kept", err, len(again), len(kept))
				}
				tc.kill(t, g[0])
			}
			if ids := liveLinkIDs(t, d, tenantID, admin); len(ids) != 0 {
				t.Fatalf("the %s link is still live: %v", tc.name, ids)
			}
			next, kept, err := r.IssueForEmail(ctx, email)
			if err != nil || len(next) != 1 || len(kept) != 0 {
				t.Fatalf("after the link was %s: %v, %d grant(s), %d kept -- want a fresh link at once",
					tc.name, err, len(next), len(kept))
			}
			if next[0].Issued.Reset.RetiredCount != 0 {
				t.Errorf("the fresh link retired %d link(s); nothing was live", next[0].Issued.Reset.RetiredCount)
			}
		})
	}
}

// TestWithdraw_RetiresOnlyALiveLinkOfItsOwnTenant: the withdrawal keeps the file's
// discipline — a spent link stays spent and is not marked retired, a second
// withdrawal keeps the first time, and another tenant's id withdraws nothing.
func TestWithdraw_RetiresOnlyALiveLinkOfItsOwnTenant(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	tenantID := newTenantRow(t, d, "Withdraw Ltd")
	other := newTenantRow(t, d, "Withdraw Other Ltd")
	resolved := func(tok ResetToken) db.ResolvedPasswordReset {
		t.Helper()
		h, err := tok.hash(r.hmacKey)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		row, err := d.GetPasswordResetByTokenHash(ctx, h)
		if err != nil {
			t.Fatalf("GetPasswordResetByTokenHash: %v", err)
		}
		return row
	}

	live := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Live")
	l, err := r.issue(ctx, tenantID, live)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := r.Withdraw(ctx, other, l.Reset.ID); err != nil {
		t.Fatalf("Withdraw(other tenant): %v", err)
	}
	if got := resolved(l.Token); got.CancelledAt != nil {
		t.Fatal("another tenant's id withdrew this tenant's link")
	}
	if err := r.Withdraw(ctx, tenantID, l.Reset.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	first := resolved(l.Token).CancelledAt
	if first == nil {
		t.Fatal("the withdrawn link is not retired")
	}
	if err := r.Withdraw(ctx, tenantID, l.Reset.ID); err != nil {
		t.Fatalf("Withdraw(again): %v", err)
	}
	if again := resolved(l.Token).CancelledAt; again == nil || !again.Equal(*first) {
		t.Errorf("a second withdrawal moved cancelled_at from %v to %v", first, again)
	}
	if _, _, err := r.Consume(ctx, l.Token, "must-not-land"); err == nil {
		t.Error("a withdrawn link could still be spent")
	}

	spentAdmin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Spent")
	s, err := r.issue(ctx, tenantID, spentAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, _, err := r.Consume(ctx, s.Token, "spent-password"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := r.Withdraw(ctx, tenantID, s.Reset.ID); err != nil {
		t.Fatalf("Withdraw(spent): %v", err)
	}
	if got := resolved(s.Token); got.UsedAt == nil || got.CancelledAt != nil {
		t.Errorf("a spent link after Withdraw: used_at %v, cancelled_at %v -- want spent and not retired",
			got.UsedAt, got.CancelledAt)
	}
}

// TestIssueForEmail_DecidesPerAccount: one address, two businesses — each account's
// link is its own. Spending one makes the next request mint for that account alone
// and keep the other's.
func TestIssueForEmail_DecidesPerAccount(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	shared := randEmail(t)
	ta := newTenantRow(t, d, "Per Account A Ltd")
	tb := newTenantRow(t, d, "Per Account B Ltd")
	a := newAdminRow(t, d, ta, shared, "p", "active", "owner", "A")
	b := newAdminRow(t, d, tb, shared, "p", "active", "owner", "B")

	g, kept, err := r.IssueForEmail(ctx, shared)
	if err != nil || len(g) != 2 || len(kept) != 0 {
		t.Fatalf("first: %v, %d grant(s), %d kept", err, len(g), len(kept))
	}
	if _, _, err := r.Consume(ctx, grantFor(t, g, a).Issued.Token, "a-new-password"); err != nil {
		t.Fatalf("Consume(A): %v", err)
	}
	g, kept, err = r.IssueForEmail(ctx, shared)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(g) != 1 || !mintedFor(g, a) {
		t.Fatalf("second request minted %d link(s); want exactly one, for A (whose link was spent)", len(g))
	}
	if len(kept) != 1 || kept[0].AdminUserID != b {
		t.Fatalf("second request kept %+v; want B's live link", kept)
	}
}

// bigPoolDB is testDB with room for every goroutine of the concurrency test at once:
// with testDB's four connections the requests would mostly queue for a connection
// rather than race inside Postgres.
func bigPoolDB(t *testing.T) *db.DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping adminauth DB tests (real Postgres required).")
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: raw + sep + "pool_max_conns=20"})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// TestIssueForEmail_ExactlyOneLinkUnderConcurrency: N requests for one account at the
// same instant mint EXACTLY one link; every other one keeps it. Without the
// per-administrator lock they all read "no live link" before any commits.
func TestIssueForEmail_ExactlyOneLinkUnderConcurrency(t *testing.T) {
	d := bigPoolDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	const rounds, callers = 8, 16
	for round := 0; round < rounds; round++ {
		tenantID := newTenantRow(t, d, "Concurrent Recovery Ltd")
		email := randEmail(t)
		admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Concurrent")

		var (
			wg      sync.WaitGroup
			start   = make(chan struct{})
			mu      sync.Mutex
			minted  int
			keptN   int
			callErr error
		)
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				g, k, err := r.IssueForEmail(ctx, email)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					callErr = err
				}
				minted += len(g)
				keptN += len(k)
			}()
		}
		close(start)
		wg.Wait()
		if callErr != nil {
			t.Fatalf("round %d: %v", round, callErr)
		}
		if minted != 1 || keptN != callers-1 {
			t.Fatalf("round %d: %d link(s) minted and %d kept by %d simultaneous requests; want 1 "+
				"and %d -- each extra mint retired the one before it and sent one more e-mail",
				round, minted, keptN, callers, callers-1)
		}
		if ids := liveLinkIDs(t, d, tenantID, admin); len(ids) != 1 {
			t.Fatalf("round %d: %d live link(s), want 1", round, len(ids))
		}
	}
}

// TestIssueForEmail_TheCeilingIsOneLinkPerResetTTL pins the relation the EM-7C note
// counts with: the per-account recovery send ceiling IS ResetTTL. A link minted on
// the shipped clock lives ResetTTL, every request inside that span keeps it, and the
// next mint is possible only when it dies — so one e-mail per ResetTTL per account.
// Changing ResetTTL changes the ceiling; this test says so where the number is.
func TestIssueForEmail_TheCeilingIsOneLinkPerResetTTL(t *testing.T) {
	if ResetTTL != time.Hour {
		t.Fatalf("ResetTTL = %v, want 1h. It is also the recovery send ceiling per account (one "+
			"e-mail per ResetTTL, ADR 0022's EM-7C note): a shorter TTL raises that ceiling, "+
			"so change the note's counted limits in the same edit.", ResetTTL)
	}
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	if r.ttl != ResetTTL {
		t.Fatalf("NewResets installed ttl %v, want ResetTTL", r.ttl)
	}
	tenantID := newTenantRow(t, d, "Ceiling Ltd")
	email := randEmail(t)
	newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Ceiling")

	g, _, err := r.IssueForEmail(ctx, email)
	if err != nil || len(g) != 1 {
		t.Fatalf("first: %v, %d grant(s)", err, len(g))
	}
	span := g[0].Issued.Reset.ExpiresAt.Sub(g[0].Issued.Reset.CreatedAt)
	// created_at is Postgres' clock and expires_at is Go's plus the TTL; a few seconds
	// of skew between the two is the slack 00019 documents.
	if span < ResetTTL-5*time.Second || span > ResetTTL+5*time.Second {
		t.Fatalf("the link lives %v, want ResetTTL (%v)", span, ResetTTL)
	}
	_, kept, err := r.IssueForEmail(ctx, email)
	if err != nil || len(kept) != 1 || !kept[0].ExpiresAt.Equal(g[0].Issued.Reset.ExpiresAt) {
		t.Fatalf("second: %v, kept %+v -- the next mint must wait for this link's expiry", err, kept)
	}
}

// TestIssueForEmail_ALinkThatExpiresDuringTheLockWaitIsNotKept is the security
// review's round-4 measurement (3/3 with now()), pinned: liveness is judged when the
// live-link READ runs — after the administrator's lock was granted — not when the
// transaction began.
//
// THE SHAPE: the owner's link is minted to die 1.5 s from now (the minting clock set
// ResetTTL - 1.5 s back). Another transaction takes the SAME administrator's lock
// (LockAdminForResetIssue — what a concurrent request holds) and keeps it 2.5 s. A
// request starts while the link is still live, waits for the lock, and reads after
// the link died. With now() (the transaction's start) it "kept" a dead link: nothing
// minted, nothing sent, the owner holding a link the consume statement refuses. With
// statement_timestamp() it mints a fresh link — and retires nothing, because the
// minting statement judges "live" on the same clock.
func TestIssueForEmail_ALinkThatExpiresDuringTheLockWaitIsNotKept(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Lock Wait Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Lock Wait")

	late := cheapResets(t, d)
	const left, held = 1500 * time.Millisecond, 2500 * time.Millisecond
	late.now = func() time.Time { return time.Now().Add(-(ResetTTL - left)) }
	g, _, err := late.IssueForEmail(ctx, email)
	if err != nil || len(g) != 1 {
		t.Fatalf("minting the dying link: %v, %d grant(s)", err, len(g))
	}
	old := g[0].Issued

	locked := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if e := store.New(tx).LockAdminForResetIssue(ctx, store.LockAdminForResetIssueParams{
				AdminUserID: admin, TenantID: tenantID,
			}); e != nil {
				return e
			}
			close(locked)
			time.Sleep(held)
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-holderDone:
		t.Fatalf("the lock holder ended before holding the lock: %v", err)
	}
	if !time.Now().Before(old.Reset.ExpiresAt) {
		t.Fatal("CONTROL FAILED: the link was already dead when the request started; the wait proves nothing")
	}

	r := cheapResets(t, d)
	grants, kept, err := r.IssueForEmail(ctx, email)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if err := <-holderDone; err != nil {
		t.Fatalf("the lock holder: %v", err)
	}
	if len(kept) != 0 {
		t.Fatalf("the request kept %+v — a link that expired while it waited for the lock; the owner would hold "+
			"a dead link and no new one would come", kept)
	}
	if len(grants) != 1 || grants[0].Issued.Reset.AdminUserID != admin {
		t.Fatalf("the request minted %d link(s), want a fresh one for the owner", len(grants))
	}
	if n := grants[0].Issued.Reset.RetiredCount; n != 0 {
		t.Errorf("the fresh link retired %d link(s); the one it replaced had already expired, and the trail "+
			"would read the expiry as somebody's request killing a pending link", n)
	}
	if _, _, err := r.Consume(ctx, old.Token, "must-not-land"); err == nil {
		t.Error("CONTROL FAILED: the old link could still be spent, so it had not expired")
	}
	if _, _, err := r.Consume(ctx, grants[0].Issued.Token, "the-owners-new-password"); err != nil {
		t.Errorf("the fresh link could not be spent: %v", err)
	}
}
