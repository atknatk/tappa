package adminauth

// identityreset_db_test.go -- M14-14 against real Postgres: the per-identity request (IssueForPerson,
// identity_open_reset), its withdrawal and its consumption (identity_consume_reset). The fixtures are
// COMMITTED by the owner (idWorld) under the operator-tables lock held shared (ownerConn ->
// holdTablesLock, this package's re-entrant holder).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// personResetWorld: one person whose identity's REGISTERED address is the upper-case spelling, two
// memberships linked to it (A active, C disabled), an UNLINKED row of the same address in B, and a
// live session in A. The rows carry the lower-case spelling; a request types a third (mixed) one.
type personResetWorld struct {
	*idWorld
	registered, typed string
	a, b, c           uuid.UUID
	rowA, rowB, rowC  uuid.UUID
}

func newPersonResetWorld(t *testing.T) personResetWorld {
	t.Helper()
	w := personResetWorld{idWorld: newIDWorld(t, ownerConn(t), true)}
	// Registered after newIDWorld, so it runs BEFORE that world's cleanup deletes the identity (the
	// links key it).
	t.Cleanup(func() {
		if _, err := w.owner.Exec(context.Background(), `DELETE FROM admin_identity_resets WHERE identity_id = $1`, w.ident); err != nil {
			t.Errorf("cleanup the identity links: %v", err)
		}
	})
	w.registered = strings.ToUpper(w.email)
	w.typed = strings.ToUpper(w.email[:1]) + w.email[1:]
	w.exec(`UPDATE admin_identities SET email = $2 WHERE id = $1`, w.ident, w.registered)
	w.a, w.b, w.c = w.tenant("M14-14 Linked Co"), w.tenant("M14-14 Unlinked Co"), w.tenant("M14-14 Disabled Co")
	w.rowA = w.linked(w.a, "active", "backfill", 72*time.Hour)
	w.rowB = w.row(w.b, w.digest, "active", 48*time.Hour)
	w.rowC = w.linked(w.c, "disabled", "backfill", 24*time.Hour)
	w.exec(`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, w.a, w.rowA, uuid.NewString())
	return w
}

func (w personResetWorld) rowLinks(row uuid.UUID) int {
	w.t.Helper()
	return w.int(`SELECT count(*) FROM password_resets WHERE admin_user_id = $1`, row)
}

// TestIssueForPerson_SendsToTheIdentitysRegisteredAddress is M14-01's inherited note (a), measured: the
// identity's link goes to admin_identities.email AS STORED -- not to the typed spelling and not to a
// row's -- and it is the identity the link was opened for; the linked rows get no per-row link, the
// unlinked row gets today's (to its own row's address); a second request finds the identity's live link
// and opens nothing (EM-7C), a withdrawn link lets the next request open a fresh one, and an unknown
// address is the empty result with a nil error.
//
// MUTATIONS (each measured red): IssueForPerson pairing the resolved id with the TYPED address; the
// unlinked-row filter removed (the linked rows get per-row links); WithdrawIdentity a no-op.
func TestIssueForPerson_SendsToTheIdentitysRegisteredAddress(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)
	r := cheapResets(t, d)

	var stored string
	if err := w.owner.QueryRow(ctx, `SELECT email::text FROM admin_identities WHERE id = $1`, w.ident).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != w.registered || stored == w.typed || stored == w.email {
		t.Fatalf("PREMISE: the three spellings are not distinct (stored %q, typed %q, rows %q)", stored, w.typed, w.email)
	}

	p, err := r.IssueForPerson(ctx, w.typed)
	if err != nil {
		t.Fatalf("IssueForPerson: %v", err)
	}
	if len(p.Grants) != 2 || p.IdentityKept != nil || len(p.Kept) != 0 {
		t.Fatalf("first request: %d grants, kept %v / %d; want the identity's link and B's row link", len(p.Grants), p.IdentityKept, len(p.Kept))
	}
	g := p.Grants[0]
	if g.IdentityID != w.ident {
		t.Fatalf("the first grant is for identity %s, want %s", g.IdentityID, w.ident)
	}
	if g.Recipient != stored {
		t.Errorf("the identity's link goes to %q, want admin_identities.email as stored (%q)", g.Recipient, stored)
	}
	if g.Issued.Reset.TenantID != uuid.Nil || g.Issued.Reset.AdminUserID != uuid.Nil {
		t.Error("the identity's link names a business or an administrator")
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE id = $1 AND identity_id = $2 AND used_at IS NULL AND cancelled_at IS NULL`, g.Issued.Reset.ID, w.ident); n != 1 {
		t.Fatalf("the grant's link is not the identity's live link (%d rows)", n)
	}
	if n := w.events("reset_requested"); n != 1 {
		t.Errorf("%d reset_requested events, want 1", n)
	}
	row := p.Grants[1]
	if row.IdentityID != uuid.Nil || row.Issued.Reset.AdminUserID != w.rowB || row.Recipient != w.email {
		t.Errorf("the second grant: identity %s, admin %s, to %q; want B's unlinked row, to its own address", row.IdentityID, row.Issued.Reset.AdminUserID, row.Recipient)
	}
	if w.rowLinks(w.rowA) != 0 || w.rowLinks(w.rowC) != 0 {
		t.Error("a row linked to the identity got a per-row link (the identity's link covers it)")
	}

	again, err := r.IssueForPerson(ctx, w.typed)
	if err != nil {
		t.Fatalf("second IssueForPerson: %v", err)
	}
	if len(again.Grants) != 0 || again.IdentityKept == nil || again.IdentityKept.ResetID != g.Issued.Reset.ID || len(again.Kept) != 1 {
		t.Fatalf("second request: %d grants, identity kept %+v, %d row kept; want nothing new, the live links kept", len(again.Grants), again.IdentityKept, len(again.Kept))
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 1 {
		t.Errorf("%d identity links after a request found the live one, want 1", n)
	}

	if err := r.WithdrawIdentity(ctx, g.Issued.Token); err != nil {
		t.Fatalf("WithdrawIdentity: %v", err)
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE id = $1 AND cancelled_at IS NOT NULL`, g.Issued.Reset.ID); n != 1 {
		t.Fatal("the withdrawn link is not cancelled")
	}
	third, err := r.IssueForPerson(ctx, w.email)
	if err != nil || len(third.Grants) != 1 || third.Grants[0].IdentityID != w.ident || third.Grants[0].Issued.Reset.ID == g.Issued.Reset.ID || third.Grants[0].Recipient != stored {
		t.Fatalf("after the withdrawal: %+v, %v; want a fresh identity link to the registered address", third, err)
	}

	none, err := r.IssueForPerson(ctx, "nobody-"+uuid.NewString()+"@identity.example")
	if err != nil || !none.Empty() {
		t.Fatalf("an unknown address: %+v, %v; want the empty result and no error", none, err)
	}
	for _, bad := range []string{"", "a\x00b@example.test", "a\xffb@example.test"} {
		if p, err := r.IssueForPerson(ctx, bad); err != nil || !p.Empty() {
			t.Errorf("IssueForPerson(%q) = %+v, %v; want the empty result", bad, p, err)
		}
	}
}

// TestIssueForPerson_NoLinkForAPersonWhoCannotSignIn: an identity whose every membership is disabled
// gets no link (the per-row path's rule for a disabled row: a link that cannot restore a sign-in is not
// minted), and nothing is written. MUTATION: the HasActiveMembership guard removed -> red.
func TestIssueForPerson_NoLinkForAPersonWhoCannotSignIn(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newIDWorld(t, ownerConn(t), true)
	t.Cleanup(func() {
		if _, err := w.owner.Exec(context.Background(), `DELETE FROM admin_identity_resets WHERE identity_id = $1`, w.ident); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	w.linked(w.tenant("M14-14 Off Co"), "disabled", "backfill", time.Hour)
	p, err := cheapResets(t, d).IssueForPerson(ctx, w.email)
	if err != nil || !p.Empty() {
		t.Fatalf("IssueForPerson = %+v, %v; want nothing", p, err)
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 0 {
		t.Errorf("%d identity links for a person who cannot sign in", n)
	}
}

// TestConsume_TheIdentityLinkResetsThePersonAndNamesTheRegisteredAddress: spending a per-identity link
// writes the identity and its linked rows (not the unlinked one), revokes the person's sessions, gives
// no session, leaves the linked business ONE admin.recovery.completed row (the definer's) and the
// unlinked business none, and returns the identity's REGISTERED address for the one notice (00075).
// A replay is ErrResetUnusable carrying the identity link's facts (used); a token no table knows is
// ErrResetUnusable with none.
//
// MUTATIONS (measured red): the identity consumption removed from Consume; the refusal's identity
// resolver removed.
func TestConsume_TheIdentityLinkResetsThePersonAndNamesTheRegisteredAddress(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)
	r := cheapResets(t, d)
	p, err := r.IssueForPerson(ctx, w.typed)
	if err != nil || len(p.Grants) == 0 || p.Grants[0].IdentityID != w.ident {
		t.Fatalf("PREMISE: no identity link (%+v, %v)", p, err)
	}
	link := p.Grants[0].Issued.Token

	c, _, err := r.Consume(ctx, link, "the-person's-new-password")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if c.IdentityID != w.ident || c.NoticeTo != w.registered || !c.Recorded || c.TenantID != uuid.Nil || c.AdminUserID != uuid.Nil || c.RevokedSessions != 1 {
		t.Fatalf("Consume = identity %s, notice to %q, recorded %v, tenant %s, admin %s, revoked %d; want the identity, its registered address, recorded, no business, 1",
			c.IdentityID, c.NoticeTo, c.Recorded, c.TenantID, c.AdminUserID, c.RevokedSessions)
	}
	if n := w.int(`SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                WHERE a.id = $1 AND a.password_hash = i.password_hash AND i.password_hash <> $2`, w.rowA, w.digest); n != 1 {
		t.Error("the linked row and the identity do not carry the same NEW digest")
	}
	if n := w.int(`SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id IS NULL AND password_hash = $2`, w.rowB, w.digest); n != 1 {
		t.Error("the unlinked row was linked or written")
	}
	if w.sessions(w.a, true) != 0 {
		t.Error("a session of the linked business survived the reset")
	}
	if w.audit(w.a, "admin.recovery.completed") != 1 || w.audit(w.b, "admin.recovery.completed") != 0 {
		t.Errorf("recovery rows: A %d, B %d; want 1, 0", w.audit(w.a, "admin.recovery.completed"), w.audit(w.b, "admin.recovery.completed"))
	}

	c, _, err = r.Consume(ctx, link, "a-second-password")
	if !errors.Is(err, ErrResetUnusable) || c.IdentityRefusal == nil || c.IdentityRefusal.IdentityID != w.ident || c.IdentityRefusal.UsedAt == nil {
		t.Fatalf("the replay: %+v, %v; want ErrResetUnusable carrying the identity link's facts (used)", c, err)
	}
	unknown, err := newResetToken()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = r.Consume(ctx, unknown, "a-third-password")
	if !errors.Is(err, ErrResetUnusable) || c.IdentityRefusal != nil {
		t.Fatalf("an unknown token: %+v, %v; want ErrResetUnusable and no identity facts", c, err)
	}
}

// faultyResetDB is the real database with injected failures (M14-14's security audit): a context
// cancelled right AFTER identity_open_reset committed, a failing row resolver, or the n-th tenant
// transaction failing.
type faultyResetDB struct {
	*db.DB
	cancelAfterOpen  context.CancelFunc
	failAdminByEmail bool
	failWithTenantAt int
	withTenantCalls  int
}

var errInjected = errors.New("injected failure")

func (f *faultyResetDB) OpenIdentityReset(ctx context.Context, id uuid.UUID, h string, exp time.Time) (db.OpenedIdentityReset, error) {
	o, err := f.DB.OpenIdentityReset(ctx, id, h, exp)
	if err == nil && f.cancelAfterOpen != nil {
		f.cancelAfterOpen()
		return db.OpenedIdentityReset{}, ctx.Err()
	}
	return o, err
}

func (f *faultyResetDB) GetAdminByEmail(ctx context.Context, email string) ([]db.ResolvedAdmin, error) {
	if f.failAdminByEmail {
		return nil, errInjected
	}
	return f.DB.GetAdminByEmail(ctx, email)
}

func (f *faultyResetDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	f.withTenantCalls++
	if f.withTenantCalls == f.failWithTenantAt {
		return errInjected
	}
	return f.DB.WithTenant(ctx, tenantID, fn)
}

// TestIssueForPerson_AFailedRequestLeavesNoLiveLink is the security audit's ORTA, measured: the
// request's context is cancelled right after identity_open_reset committed (an attacker dropping the
// connection). The request fails -- and leaves NO live link: the identity's link is withdrawn by its
// own hash and the per-row link minted before it is withdrawn too, so the owner's next request mints
// fresh links instead of answering "kept" for ResetTTL. A failing row resolver mints nothing at all
// (the identity's link is minted last). MUTATIONS (measured red): the withdrawal in openForIdentity
// removed; withdrawAfterFailure in IssueForPerson removed; the identity's mint moved back before the
// row resolver.
func TestIssueForPerson_AFailedRequestLeavesNoLiveLink(t *testing.T) {
	d := testDB(t)
	w := newPersonResetWorld(t)
	liveIdentity := func() int {
		return w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1 AND used_at IS NULL AND cancelled_at IS NULL AND expires_at > now()`, w.ident)
	}
	liveRowB := func() int {
		return w.int(`SELECT count(*) FROM password_resets WHERE admin_user_id = $1 AND used_at IS NULL AND cancelled_at IS NULL AND expires_at > now()`, w.rowB)
	}

	t.Run("the row resolver fails: nothing is minted", func(t *testing.T) {
		r := cheapResets(t, &faultyResetDB{DB: d, failAdminByEmail: true})
		if _, err := r.IssueForPerson(context.Background(), w.typed); !errors.Is(err, errInjected) {
			t.Fatalf("IssueForPerson = %v, want the injected failure", err)
		}
		if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 0 {
			t.Errorf("%d identity links minted by a request that failed before its end", n)
		}
	})

	t.Run("cancelled right after the identity's mint: everything withdrawn", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := cheapResets(t, &faultyResetDB{DB: d, cancelAfterOpen: cancel})
		if _, err := r.IssueForPerson(ctx, w.typed); !errors.Is(err, context.Canceled) {
			t.Fatalf("IssueForPerson = %v, want context.Canceled", err)
		}
		if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 1 {
			t.Fatalf("PREMISE: %d identity links written, want the one the cancelled request committed", n)
		}
		if n := liveIdentity(); n != 0 {
			t.Errorf("%d live identity links after the failed request, want 0 (an orphan blocks the owner for ResetTTL)", n)
		}
		if n := liveRowB(); n != 0 {
			t.Errorf("%d live per-row links after the failed request, want 0", n)
		}
		next, err := cheapResets(t, d).IssueForPerson(context.Background(), w.typed)
		if err != nil || next.IdentityKept != nil || len(next.Grants) != 2 || next.Grants[0].IdentityID != w.ident {
			t.Fatalf("the owner's next request: %+v, %v; want a fresh identity link and B's row link, nothing kept", next, err)
		}
	})
}

// TestIssueForEmail_ASecondRowFailureWithdrawsTheFirstRowsLink: the per-row path's pre-existing orphan
// (the same audit): two rows of one address, the second row's transaction fails -> the request fails
// and the first row's link, already committed, is withdrawn; the next request mints for both.
// MUTATION: withdrawAfterFailure in issueForRows removed -> red.
func TestIssueForEmail_ASecondRowFailureWithdrawsTheFirstRowsLink(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	email := randEmail(t)
	first := newAdminRow(t, d, newTenantRow(t, d, "Orphan One Ltd"), email, "p", "active", "owner", "First")
	newAdminRow(t, d, newTenantRow(t, d, "Orphan Two Ltd"), email, "p", "active", "owner", "Second")
	// Per row: the address read (one transaction), then the mint (one). The third is the second row's read.
	r := cheapResets(t, &faultyResetDB{DB: d, failWithTenantAt: 3})
	if _, _, err := r.IssueForEmail(ctx, email); !errors.Is(err, errInjected) {
		t.Fatalf("IssueForEmail = %v, want the injected failure", err)
	}
	owner := ownerConn(t)
	var total, live int
	if err := owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE used_at IS NULL AND cancelled_at IS NULL) FROM password_resets WHERE admin_user_id = $1`, first).Scan(&total, &live); err != nil {
		t.Fatal(err)
	}
	if total != 1 || live != 0 {
		t.Errorf("the first row: %d links, %d live; want its one link withdrawn", total, live)
	}
	grants, kept, err := cheapResets(t, d).IssueForEmail(ctx, email)
	if err != nil || len(grants) != 2 || len(kept) != 0 {
		t.Fatalf("the next request: %d grants, %d kept, %v; want both rows minted", len(grants), len(kept), err)
	}
}
