package adminauth

// admininvite_db_test.go -- the administrator invitation (M14-15, ADR 0031 §5.11, migration 00077)
// against real Postgres, through AdminInvites on the APPLICATION pool (tappa_app), the worlds written
// and cleaned up as the owner (idWorld). Committed: the S13 race needs two real transactions.
//
// The acceptances are 00077's functions; this file holds their outcomes as the product sees them --
// the membership born (or the refused row relinked), the forwarded link refused, the address already
// in the business refused, the mailbox proof, no session, the GET's lookup consuming nothing, and a
// concurrent acceptance and registration of one new address ending in one identity and no error.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// inviteWorld is one business (with its owner row) that invites, around an idWorld (the invitee).
type inviteWorld struct {
	*idWorld
	s        *AdminInvites
	tenant   uuid.UUID
	ownerRow uuid.UUID
}

// newInviteWorld takes the owner connection (ownerConn -- the operator-tables holder) from its caller,
// so the caller holds the lock before any row is written (cmd/opadmin's TablesLock scan).
func newInviteWorld(t *testing.T, owner *pgx.Conn, withIdentity bool) *inviteWorld {
	t.Helper()
	d := testDB(t)
	s, err := NewAdminInvites(d, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	s.digest = func(pw string) (string, error) { return fixtureDigest(t, pw), nil }
	w := &inviteWorld{idWorld: newIDWorld(t, owner, withIdentity), s: s}
	w.tenant = w.business("M14-15 Inviting Co")
	w.ownerRow = uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'M14-15 Owner', $3, $4, 'owner')`,
		w.ownerRow, w.tenant, "owner-"+uuid.NewString()+"@identity.example", fixtureDigest(t, "owner"))
	return w
}

// business makes a business whose invitations are deleted at cleanup (00077's Down refuses while one
// exists) and whose rows of the invitee's address are unlinked first (idWorld's cleanup does that).
func (w *inviteWorld) business(name string) uuid.UUID {
	w.t.Helper()
	id := w.idWorld.tenant(name)
	w.t.Cleanup(func() {
		if _, err := w.owner.Exec(context.Background(), `DELETE FROM admin_invites WHERE tenant_id = $1`, id); err != nil {
			w.t.Errorf("cleanup invites: %v", err)
		}
	})
	return id
}

// cleanIdentityOf cleans up an identity the test BIRTHED (acceptance with a new password).
func (w *inviteWorld) cleanIdentityOf(email string) {
	w.t.Cleanup(func() {
		ctx := context.Background()
		for _, sql := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL
			  WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`,
			`DELETE FROM admin_identity_events WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`,
			`DELETE FROM admin_identities WHERE email = $1`,
		} {
			if _, err := w.owner.Exec(ctx, sql, email); err != nil {
				w.t.Errorf("cleanup %s: %v", sql, err)
			}
		}
	})
}

// session inserts a live session for row and returns its token (hashed under the panel's key).
func (w *inviteWorld) session(tenant, row uuid.UUID) Token {
	w.t.Helper()
	tok, err := newToken()
	if err != nil {
		w.t.Fatal(err)
	}
	h, err := tok.hash(w.s.sessionKey)
	if err != nil {
		w.t.Fatal(err)
	}
	w.exec(`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, tenant, row, h)
	return tok
}

func (w *inviteWorld) issue(tenant, by uuid.UUID, email string) IssuedInvite {
	w.t.Helper()
	is, err := w.s.Issue(context.Background(), tenant, by, email, "M14-15 Invitee")
	if err != nil {
		w.t.Fatalf("Issue: %v", err)
	}
	return is
}

func (w *inviteWorld) inviteState(id uuid.UUID) string {
	w.t.Helper()
	var used, cancelled bool
	if err := w.owner.QueryRow(context.Background(), `SELECT used_at IS NOT NULL, cancelled_at IS NOT NULL FROM admin_invites WHERE id = $1`, id).Scan(&used, &cancelled); err != nil {
		w.t.Fatal(err)
	}
	switch {
	case used:
		return "used"
	case cancelled:
		return "cancelled"
	}
	return "open"
}

// rowsOf counts tenant's admin_users rows of the invitee's address.
func (w *inviteWorld) rowsOf(tenant uuid.UUID, email string) int {
	return w.int(`SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND email = $2`, tenant, email)
}

// TestAdminInvites_IssuingWritesNoMembershipAndNoIdentity: an invitation to an address with an identity
// and one to an address without; before any acceptance the business holds NO row of either address
// and no identity was born or touched (the card's "kabul edilmeden admin_users satırı 0"); both are
// "open" on the list with the same fields; no trail row carries an address (the handler writes the
// issuance's row once the e-mail's fate is known).
func TestAdminInvites_IssuingWritesNoMembershipAndNoIdentity(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, true)
	stranger := "m14-15-new-" + uuid.NewString() + "@identity.example"
	identitiesBefore := w.int(`SELECT count(*) FROM admin_identities`)
	known, unknown := w.issue(w.tenant, w.ownerRow, w.email), w.issue(w.tenant, w.ownerRow, stranger)
	if n := w.rowsOf(w.tenant, w.email) + w.rowsOf(w.tenant, stranger); n != 0 {
		t.Fatalf("the business holds %d rows of the invited addresses before any acceptance, want 0", n)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities`); n != identitiesBefore {
		t.Fatalf("issuing changed the identity count %d -> %d", identitiesBefore, n)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities WHERE email = $1`, stranger); n != 0 {
		t.Fatalf("issuing birthed an identity for the new address")
	}
	list, err := w.s.List(context.Background(), w.tenant)
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v; want 2", len(list), err)
	}
	for _, a := range list {
		if a.State(time.Now()) != InviteStateOpen || a.Role != InviteRoleManager || a.UsedAt != nil || a.CancelledAt != nil {
			t.Errorf("invitation %s on the list: %+v", a.ID, a)
		}
	}
	if known.Invite.ExpiresAt.Sub(known.Invite.CreatedAt).Round(time.Minute) != unknown.Invite.ExpiresAt.Sub(unknown.Invite.CreatedAt).Round(time.Minute) {
		t.Error("the two invitations' lifetimes differ")
	}
	if n := w.int(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND (detail::text ILIKE '%' || $2 || '%' OR detail::text ILIKE '%' || $3 || '%')`, w.tenant, w.email, stranger); n != 0 {
		t.Errorf("%d audit rows carry an invited address", n)
	}
}

// TestAdminInvites_ASecondInvitationRetiresTheOpenOne: one open invitation per address per business.
func TestAdminInvites_ASecondInvitationRetiresTheOpenOne(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, false)
	first := w.issue(w.tenant, w.ownerRow, w.email)
	second := w.issue(w.tenant, w.ownerRow, strings.ToUpper(w.email))
	if w.inviteState(first.Invite.ID) != "cancelled" || w.inviteState(second.Invite.ID) != "open" || second.Retired != 1 {
		t.Fatalf("first %s, second %s, retired %d; want cancelled, open, 1", w.inviteState(first.Invite.ID), w.inviteState(second.Invite.ID), second.Retired)
	}
	if err := w.s.Usable(context.Background(), first.Token); !errors.Is(err, ErrInviteUnusable) {
		t.Errorf("the retired link answers %v", err)
	}
}

// TestAdminInvites_LookingAtTheLinkConsumesNothing (R19): Usable -- the GET's question -- three
// times, and the invitation is still open and still accepted afterwards.
func TestAdminInvites_LookingAtTheLinkConsumesNothing(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, false)
	is := w.issue(w.tenant, w.ownerRow, w.email)
	w.cleanIdentityOf(w.email)
	for i := 0; i < 3; i++ {
		if err := w.s.Usable(context.Background(), is.Token); err != nil {
			t.Fatalf("look %d: %v", i, err)
		}
	}
	if st := w.inviteState(is.Invite.ID); st != "open" {
		t.Fatalf("after three looks the invitation is %s", st)
	}
	if _, err := w.s.AcceptNew(context.Background(), is.Token, "a new password"); err != nil {
		t.Fatalf("accept after the looks: %v", err)
	}
	if err := w.s.Usable(context.Background(), is.Token); !errors.Is(err, ErrInviteUnusable) {
		t.Errorf("a spent link looks %v", err)
	}
	if err := w.s.Usable(context.Background(), ParseInviteToken("junk")); !errors.Is(err, ErrInviteUnusable) {
		t.Errorf("a malformed link looks %v", err)
	}
}

// TestAdminInvites_AnExistingIdentityAcceptsWithItsSession: the identity (unverified) is signed in to
// business A; the invitation into B is accepted with that session -> a membership in B, linked_via
// 'invite', the identity's digest copied, the invitation spent, the mailbox proved (email_verified_at
// and a first_verify window, its event row), ONE admin.invite.accepted row in B of the same shape the
// new-address path writes, and NO session in B. Accepted again: unusable. A verified identity's click
// changes nothing about the identity.
func TestAdminInvites_AnExistingIdentityAcceptsWithItsSession(t *testing.T) {
	owner := ownerConn(t)
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprintf("verified=%v", verified), func(t *testing.T) {
			w := newInviteWorld(t, owner, true)
			a := w.business("M14-15 A")
			rowA := w.linked(a, "active", "backfill", 48*time.Hour)
			if verified {
				w.exec(`UPDATE admin_identities SET email_verified_at = now() - interval '9 days' WHERE id = $1`, w.ident)
			}
			var beforeVerified, beforeOpened *time.Time
			read := func() (*time.Time, *time.Time) {
				var v, o *time.Time
				if err := w.owner.QueryRow(context.Background(), `SELECT email_verified_at, confirm_opened_at FROM admin_identities WHERE id = $1`, w.ident).Scan(&v, &o); err != nil {
					t.Fatal(err)
				}
				return v, o
			}
			beforeVerified, beforeOpened = read()
			tok := w.session(a, rowA)
			is := w.issue(w.tenant, w.ownerRow, w.email)
			got, err := w.s.AcceptWithSession(context.Background(), is.Token, tok)
			if err != nil || got.TenantID != w.tenant {
				t.Fatalf("AcceptWithSession = %+v, %v", got, err)
			}
			ident, via := w.link(got.AdminUserID)
			if ident != w.ident || via != "invite" {
				t.Fatalf("the new row's link: %v %q", ident, via)
			}
			if n := w.int(`SELECT count(*) FROM admin_users u JOIN admin_identities i ON i.id = u.identity_id
			                WHERE u.id = $1 AND u.password_hash = i.password_hash AND u.role = 'manager' AND u.status = 'active'
			                  AND u.full_name = 'M14-15 Invitee'`, got.AdminUserID); n != 1 {
				t.Error("the new row is not an active manager carrying the identity's digest and the invited name")
			}
			if st := w.inviteState(is.Invite.ID); st != "used" {
				t.Errorf("the invitation is %s, want used", st)
			}
			if n := w.sessions(w.tenant, false); n != 0 {
				t.Errorf("the acceptance opened %d sessions in the business, want 0 (it gives none)", n)
			}
			if n := w.audit(w.tenant, "admin.invite.accepted"); n != 1 {
				t.Errorf("%d admin.invite.accepted rows, want 1", n)
			}
			v, o := read()
			if verified {
				if !v.Equal(*beforeVerified) || (o != nil) != (beforeOpened != nil) || w.events("email_verified") != 0 {
					t.Errorf("a verified identity's click changed it: verified %v -> %v, window %v -> %v", beforeVerified, v, beforeOpened, o)
				}
			} else {
				var scope *string
				if err := w.owner.QueryRow(context.Background(), `SELECT confirm_scope FROM admin_identities WHERE id = $1`, w.ident).Scan(&scope); err != nil {
					t.Fatal(err)
				}
				if v == nil || o == nil || scope == nil || *scope != "first_verify" || w.events("email_verified") != 1 {
					t.Errorf("the mailbox proof: verified %v, window %v scope %v, events %d", v, o, scope, w.events("email_verified"))
				}
			}
			if _, err := w.s.AcceptWithSession(context.Background(), is.Token, tok); !errors.Is(err, ErrInviteUnusable) {
				t.Errorf("a second acceptance answers %v, want ErrInviteUnusable", err)
			}
		})
	}
}

// TestAdminInvites_AForwardedLinkAcceptsNothing (bulgu 12): the invitation names the invitee's address;
// another identity's live session clicks it -> ErrInviteNotAccepted, no row of either address in the
// business, the invitation still open, no identity changed.
func TestAdminInvites_AForwardedLinkAcceptsNothing(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, true)
	other := newIDWorld(t, w.owner, true)
	c := w.business("M14-15 C")
	rowC := other.linked(c, "active", "backfill", 48*time.Hour)
	tok := w.session(c, rowC)
	is := w.issue(w.tenant, w.ownerRow, w.email)
	if _, err := w.s.AcceptWithSession(context.Background(), is.Token, tok); !errors.Is(err, ErrInviteNotAccepted) {
		t.Fatalf("a forwarded link with another person's session answers %v, want ErrInviteNotAccepted", err)
	}
	if n := w.rowsOf(w.tenant, w.email) + w.rowsOf(w.tenant, other.email); n != 0 {
		t.Fatalf("the forwarded link made %d rows", n)
	}
	if st := w.inviteState(is.Invite.ID); st != "open" {
		t.Errorf("the invitation is %s, want open", st)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities WHERE id IN ($1, $2) AND email_verified_at IS NOT NULL`, w.ident, other.ident); n != 0 {
		t.Errorf("the forwarded click verified %d identities", n)
	}
	// Without any session (a bare visitor) -> the same refusal.
	if _, err := w.s.AcceptWithSession(context.Background(), is.Token, Token{}); !errors.Is(err, ErrInviteNotAccepted) {
		t.Errorf("no session answers %v", err)
	}
}

// TestAdminInvites_ARefusedRowIsRelinked (ONAY note 3 and the 2026-10-27 audits' YÜKSEK finding): the
// business holds a REFUSED row of the address (the person unlinked it from the confirmation list:
// unlinked_at, a sentinel digest, no link).
//   - a MANAGER row: a manager invitation accepted with the person's session relinks THAT row -- same
//     id, role 'manager', linked_via 'invite', unlinked_at NULL, the identity's digest, active -- and no
//     second row exists;
//   - an OWNER row: the manager invitation brings nothing back -- ErrInviteNotAccepted, the row still
//     refused and an owner, unlinked, the invitation open. MUTATION (measured): the role predicate
//     removed from the relink gate -> red (the owner row came back as an active owner).
func TestAdminInvites_ARefusedRowIsRelinked(t *testing.T) {
	for _, role := range []string{"manager", "owner"} {
		t.Run(role, func(t *testing.T) {
			owner := ownerConn(t)
			w := newInviteWorld(t, owner, true)
			a := w.business("M14-15 A")
			rowA := w.linked(a, "active", "backfill", 48*time.Hour)
			refused := w.row(w.tenant, fixtureDigest(t, "sentinel"), "active", 72*time.Hour)
			w.exec(`UPDATE admin_users SET unlinked_at = now() - interval '1 day', role = $2 WHERE id = $1`, refused, role)
			is := w.issue(w.tenant, w.ownerRow, w.email)
			got, err := w.s.AcceptWithSession(context.Background(), is.Token, w.session(a, rowA))
			if role == "owner" {
				if !errors.Is(err, ErrInviteNotAccepted) {
					t.Fatalf("a manager invitation over a refused OWNER row answered %+v, %v; want ErrInviteNotAccepted", got, err)
				}
				if n := w.int(`SELECT count(*) FROM admin_users WHERE id = $1 AND role = 'owner' AND identity_id IS NULL AND unlinked_at IS NOT NULL`, refused); n != 1 {
					t.Error("the refused owner row changed")
				}
				if st := w.inviteState(is.Invite.ID); st != "open" {
					t.Errorf("the invitation is %s, want open", st)
				}
				return
			}
			if err != nil {
				t.Fatalf("AcceptWithSession: %v", err)
			}
			if got.AdminUserID != refused {
				t.Fatalf("the acceptance made %s, want the refused row %s relinked", got.AdminUserID, refused)
			}
			if n := w.int(`SELECT count(*) FROM admin_users u JOIN admin_identities i ON i.id = u.identity_id
			                WHERE u.id = $1 AND u.linked_via = 'invite' AND u.unlinked_at IS NULL AND u.status = 'active'
			                  AND u.role = 'manager' AND u.password_hash = i.password_hash`, refused); n != 1 {
				t.Error("the refused manager row is not relinked as an active manager with the identity's digest")
			}
			if n := w.rowsOf(w.tenant, w.email); n != 1 {
				t.Errorf("%d rows of the address in the business, want 1", n)
			}
		})
	}
}

// TestAdminInvites_AnAddressAlreadyInTheBusinessIsNotAcceptedAgain: the business holds an active row of
// the address -- unlinked, or linked to the person -- so the acceptance is refused with the one
// answer, nothing is written, and the invitation stays open (the owner sees what an ignored e-mail
// shows -- R18).
func TestAdminInvites_AnAddressAlreadyInTheBusinessIsNotAcceptedAgain(t *testing.T) {
	owner := ownerConn(t)
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprintf("linked=%v", linked), func(t *testing.T) {
			w := newInviteWorld(t, owner, true)
			a := w.business("M14-15 A")
			rowA := w.linked(a, "active", "backfill", 48*time.Hour)
			if linked {
				w.linked(w.tenant, "active", "backfill", 48*time.Hour)
			} else {
				w.row(w.tenant, fixtureDigest(t, "legacy"), "active", time.Hour)
			}
			is := w.issue(w.tenant, w.ownerRow, w.email)
			if _, err := w.s.AcceptWithSession(context.Background(), is.Token, w.session(a, rowA)); !errors.Is(err, ErrInviteNotAccepted) {
				t.Fatalf("answers %v, want ErrInviteNotAccepted", err)
			}
			if n := w.rowsOf(w.tenant, w.email); n != 1 {
				t.Errorf("%d rows of the address, want the one there was", n)
			}
			if st := w.inviteState(is.Invite.ID); st != "open" {
				t.Errorf("the invitation is %s, want open", st)
			}
		})
	}
}

// TestAdminInvites_ANewAddressChoosesItsPassword: no identity carries the address; the invitee chooses a
// password -> an identity born verified with that digest, its one membership in the business
// (linked_via 'invite'), no session; the link is spent. For an address that HAS an identity the same
// path births nothing and changes nothing (the person signs in instead).
func TestAdminInvites_ANewAddressChoosesItsPassword(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, false)
	w.cleanIdentityOf(w.email)
	is := w.issue(w.tenant, w.ownerRow, w.email)
	got, err := w.s.AcceptNew(context.Background(), is.Token, "the invitee's own")
	if err != nil {
		t.Fatalf("AcceptNew: %v", err)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities i JOIN admin_users u ON u.identity_id = i.id
	                WHERE i.email = $1 AND i.email_verified_at IS NOT NULL AND u.id = $2 AND u.linked_via = 'invite'
	                  AND u.password_hash = i.password_hash AND u.role = 'manager' AND u.tenant_id = $3`, w.email, got.AdminUserID, w.tenant); n != 1 {
		t.Fatal("no verified identity with its invited membership")
	}
	if n := w.sessions(w.tenant, false); n != 0 {
		t.Errorf("%d sessions, want 0", n)
	}
	if st := w.inviteState(is.Invite.ID); st != "used" {
		t.Errorf("the invitation is %s", st)
	}

	known := newInviteWorld(t, owner, true)
	is2 := known.issue(known.tenant, known.ownerRow, known.email)
	var digest string
	if err := known.owner.QueryRow(context.Background(), `SELECT password_hash FROM admin_identities WHERE id = $1`, known.ident).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if _, err := known.s.AcceptNew(context.Background(), is2.Token, "an attacker's choice"); !errors.Is(err, ErrInviteNotAccepted) {
		t.Fatalf("a new password for a known address answers %v, want ErrInviteNotAccepted", err)
	}
	if n := known.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2 AND email_verified_at IS NULL`, known.ident, digest); n != 1 {
		t.Error("the known identity changed")
	}
	if n := known.rowsOf(known.tenant, known.email); n != 0 || known.inviteState(is2.Invite.ID) != "open" {
		t.Errorf("rows %d, invitation %s; want 0 and open", n, known.inviteState(is2.Invite.ID))
	}
}

// TestAdminInvites_ACancelledOrExpiredLinkIsUnusable: cancel (the owner's) and expiry each make the link
// unusable on both paths; a cancel of a spent or unknown invitation reports false.
func TestAdminInvites_ACancelledOrExpiredLinkIsUnusable(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, false)
	c := w.issue(w.tenant, w.ownerRow, w.email)
	ok, err := w.s.Cancel(context.Background(), w.tenant, c.Invite.ID)
	if err != nil || !ok {
		t.Fatalf("Cancel = %v, %v", ok, err)
	}
	if ok, _ := w.s.Cancel(context.Background(), w.tenant, c.Invite.ID); ok {
		t.Error("a second cancel reported true")
	}
	if ok, _ := w.s.Cancel(context.Background(), uuid.New(), c.Invite.ID); ok {
		t.Error("another business cancelled this business's invitation")
	}
	if _, err := w.s.AcceptNew(context.Background(), c.Token, "pw pw pw pw"); !errors.Is(err, ErrInviteUnusable) {
		t.Errorf("a cancelled link answers %v", err)
	}
	e := w.issue(w.tenant, w.ownerRow, w.email)
	w.exec(`UPDATE admin_invites SET created_at = now() - interval '4 days', expires_at = now() - interval '1 minute' WHERE id = $1`, e.Invite.ID)
	if _, err := w.s.AcceptNew(context.Background(), e.Token, "pw pw pw pw"); !errors.Is(err, ErrInviteUnusable) {
		t.Errorf("an expired link answers %v", err)
	}
	if n := w.rowsOf(w.tenant, w.email); n != 0 {
		t.Errorf("%d rows", n)
	}
}

// TestAdminInvites_ConcurrentAcceptanceAndRegistrationOfOneAddress is S13 (ADR 0031 §5.2): an invitee
// accepts with a NEW password while, at the same instant, a registration of the SAME new address commits
// (identity_create_for_signup in its own transaction -- the registration's dual-write until M14-17;
// M14-17's claim takes the same lock). Since M14-19 (00083) tappa_app holds no EXECUTE on it and no
// binary calls it, so the registration calls it as the migration role -- the function and its lock
// stay until contract B, and the race is still the body's. N rounds, each a barrier and
// two goroutines (-race). Every round ends with NO error but the acceptance's ordinary refusal, and
// exactly ONE identity of the address: the acceptance's (the registration's row unlinked) or the
// registration's (the acceptance refused, nothing of it written). A second pair races two acceptances
// of one link (a double click): one wins, the other is told the link is spent.
func TestAdminInvites_ConcurrentAcceptanceAndRegistrationOfOneAddress(t *testing.T) {
	owner := ownerConn(t)
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set")
	}
	const rounds = 12
	outcomes := map[string]int{}
	for i := 0; i < rounds; i++ {
		w := newInviteWorld(t, owner, false)
		w.cleanIdentityOf(w.email)
		is := w.issue(w.tenant, w.ownerRow, w.email)
		signupTenant := uuid.New()
		signupRow := uuid.New()
		reg, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reg.Close(context.Background()) })

		var (
			wg             sync.WaitGroup
			startAccept    = make(chan struct{})
			startRegister  = make(chan struct{})
			acceptErr      error
			linked         bool
			registerErr    error
			acceptedTenant uuid.UUID
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startAccept
			a, err := w.s.AcceptNew(context.Background(), is.Token, "raced password")
			acceptErr, acceptedTenant = err, a.TenantID
		}()
		go func() {
			defer wg.Done()
			<-startRegister
			registerErr = pgx.BeginFunc(context.Background(), reg, func(tx pgx.Tx) error {
				ctx := context.Background()
				if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'M14-15 S13', $2, 'bar', 'single')`,
					signupTenant, "VAT-M1415-"+signupTenant.String()); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'S13 Owner', $3, $4, 'owner')`,
					signupRow, signupTenant, w.email, w.digest); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, signupTenant.String()); err != nil {
					return err
				}
				return tx.QueryRow(ctx, `SELECT identity_create_for_signup($1, $2)`, signupTenant, signupRow).Scan(&linked)
			})
		}()
		// THE GATE: a third session holds the address's lock (the expression both functions take) while
		// the two callers queue on it -- one, then the other, alternating by round -- and then lets go.
		// Both are INSIDE their transactions, waiting at the same lock at the same time; the queue is
		// first-come, so the rounds cover both orders: registration first in the even rounds, the
		// acceptance first in the odd ones.
		gate, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = gate.Close(context.Background()) })
		var key int64
		if err := gate.QueryRow(context.Background(), `SELECT hashtextextended('admin-identity-email:' || lower($1), 0)`, w.email).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if _, err := gate.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, key); err != nil {
			t.Fatal(err)
		}
		// NO t.Fatal WHILE THE TWO RUN (S13 hygiene): a failure here lets the gate go, closes whichever
		// start is still shut and waits for both goroutines before the test stops -- none outlives it.
		started := map[chan struct{}]bool{}
		start := func(c chan struct{}) { close(c); started[c] = true }
		stop := func(format string, args ...any) {
			t.Helper()
			if _, err := gate.Exec(context.Background(), `SELECT pg_advisory_unlock_all()`); err != nil {
				t.Errorf("let the gate go: %v", err)
			}
			for _, c := range []chan struct{}{startAccept, startRegister} {
				if !started[c] {
					close(c)
				}
			}
			wg.Wait()
			t.Fatalf(format, args...)
		}
		waitFor := func(n int) {
			deadline := time.Now().Add(15 * time.Second)
			for {
				var waiting int
				if err := gate.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks l WHERE l.locktype = 'advisory' AND NOT l.granted
				                                                AND l.classid = ((($1::int8) >> 32) & 4294967295)::oid AND l.objid = (($1::int8) & 4294967295)::oid`, key).Scan(&waiting); err != nil {
					stop("round %d: read the lock queue: %v", i, err)
				}
				if waiting >= n {
					return
				}
				if time.Now().After(deadline) {
					stop("round %d: %d of %d callers reached the address's lock", i, waiting, n)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
		first, second := startRegister, startAccept
		if i%2 == 1 {
			first, second = startAccept, startRegister
		}
		start(first)
		waitFor(1)
		start(second)
		waitFor(2)
		if _, err := gate.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key); err != nil {
			stop("round %d: let the gate go: %v", i, err)
		}
		wg.Wait()
		if registerErr != nil {
			t.Fatalf("round %d: the registration failed: %v", i, registerErr)
		}
		if acceptErr != nil && !errors.Is(acceptErr, ErrInviteNotAccepted) {
			t.Fatalf("round %d: the acceptance failed with %v -- S13: no 500", i, acceptErr)
		}
		if n := w.int(`SELECT count(*) FROM admin_identities WHERE email = $1`, w.email); n != 1 {
			t.Fatalf("round %d: %d identities of the address, want exactly 1", i, n)
		}
		switch {
		case acceptErr == nil && !linked && acceptedTenant == w.tenant:
			outcomes["acceptance first"]++
		case acceptErr != nil && linked && w.rowsOf(w.tenant, w.email) == 0:
			outcomes["registration first"]++
		default:
			t.Fatalf("round %d: acceptance %v (tenant %v), registration linked=%v -- not one of the two consistent outcomes", i, acceptErr, acceptedTenant, linked)
		}
		w.exec(`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`, signupRow)
	}
	t.Logf("S13 outcomes over %d rounds: %v", rounds, outcomes)
	if outcomes["acceptance first"] != rounds/2 || outcomes["registration first"] != rounds/2 {
		t.Errorf("the queue's order did not decide the rounds (%v); want %d of each", outcomes, rounds/2)
	}

	// The double click.
	w := newInviteWorld(t, owner, false)
	w.cleanIdentityOf(w.email)
	is := w.issue(w.tenant, w.ownerRow, w.email)
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  [2]error
	)
	for k := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[k] = w.s.AcceptNew(context.Background(), is.Token, "double click")
		}()
	}
	close(start)
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrInviteUnusable), errors.Is(err, ErrInviteNotAccepted):
		default:
			t.Fatalf("a double click failed with %v", err)
		}
	}
	if wins != 1 || w.rowsOf(w.tenant, w.email) != 1 {
		t.Fatalf("double click: %d wins, %d rows; want 1 and 1", wins, w.rowsOf(w.tenant, w.email))
	}
}

// TestAdminInvites_OnlyAnActiveOwnerRowIssues is the INSERT's belt (admininvites.sql): the invitation is
// written FROM the inviter's own row, so a manager's row, a disabled owner's and another business's
// owner write nothing (ErrInviteNotOwner). MUTATION (measured): the role predicate removed -> red.
func TestAdminInvites_OnlyAnActiveOwnerRowIssues(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, false)
	manager := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'M14-15 Manager', $3, $4, 'manager')`,
		manager, w.tenant, "mgr-"+uuid.NewString()+"@identity.example", fixtureDigest(t, "m"))
	other := w.business("M14-15 Other")
	for name, c := range map[string]struct{ tenant, by uuid.UUID }{
		"a manager": {w.tenant, manager}, "another business's owner": {other, w.ownerRow},
	} {
		if _, err := w.s.Issue(context.Background(), c.tenant, c.by, w.email, "X"); !errors.Is(err, ErrInviteNotOwner) {
			t.Errorf("%s: %v, want ErrInviteNotOwner", name, err)
		}
	}
	w.exec(`UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.ownerRow)
	if _, err := w.s.Issue(context.Background(), w.tenant, w.ownerRow, w.email, "X"); !errors.Is(err, ErrInviteNotOwner) {
		t.Errorf("a disabled owner: %v", err)
	}
	if n := w.int(`SELECT count(*) FROM admin_invites WHERE tenant_id IN ($1, $2)`, w.tenant, other); n != 0 {
		t.Errorf("%d invitations written", n)
	}
}

// TestAdminInvites_ANonASCIIAddressIsNeverStored is the 2026-10-26 address rule at the owner's form:
// an address the mail would refuse -- a Kelvin sign or a dotted capital I that citext folds onto ASCII,
// a space, a display name -- is ErrInviteAddress BEFORE anything is read or written (no row, no lookup:
// R18 -- the refusal is about the form, never about an identity). MUTATION (measured): the
// mail.ValidRecipient check removed from Issue -> red.
func TestAdminInvites_ANonASCIIAddressIsNeverStored(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, false)
	local := "m14-15-" + uuid.NewString()[:8]
	for _, bad := range []string{local + "K@identity.example", local + "İ@identity.example", local + " x@identity.example",
		"Maria <" + local + "@identity.example>"} {
		if _, err := w.s.Issue(context.Background(), w.tenant, w.ownerRow, bad, "X"); !errors.Is(err, ErrInviteAddress) {
			t.Errorf("%q: %v, want ErrInviteAddress", bad, err)
		}
	}
	if n := w.int(`SELECT count(*) FROM admin_invites WHERE tenant_id = $1`, w.tenant); n != 0 {
		t.Errorf("%d invitations stored", n)
	}
	if _, err := w.s.Issue(context.Background(), w.tenant, w.ownerRow, local+"k@identity.example", "X"); err != nil {
		t.Errorf("CONTROL: the ASCII address was refused: %v", err)
	}
}

// TestAdminInvites_ASuspendedBusinessTakesNoNewManager is the user's decision of 2026-10-27: while the
// inviting business is suspended, BOTH acceptances refuse -- ErrInviteNotNow (the page says "not
// now"), no row, no identity, the invitation still open -- and once the suspension is lifted the same
// link works. MUTATION (measured): the suspension read removed from either function -> red.
func TestAdminInvites_ASuspendedBusinessTakesNoNewManager(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, true)
	a := w.business("M14-15 A")
	rowA := w.linked(a, "active", "backfill", 48*time.Hour)
	stranger := "m14-15-susp-" + uuid.NewString() + "@identity.example"
	w.cleanIdentityOf(stranger)
	withSession, fresh := w.issue(w.tenant, w.ownerRow, w.email), w.issue(w.tenant, w.ownerRow, stranger)
	w.exec(`UPDATE tenants SET suspended_at = now(), suspension_reason = 'security_hold' WHERE id = $1`, w.tenant)
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, w.tenant); err != nil {
			t.Errorf("lift the suspension: %v", err)
		}
	})
	session := w.session(a, rowA)
	if _, err := w.s.AcceptWithSession(context.Background(), withSession.Token, session); !errors.Is(err, ErrInviteNotNow) {
		t.Errorf("the session acceptance into a suspended business answered %v, want ErrInviteNotNow", err)
	}
	if _, err := w.s.AcceptNew(context.Background(), fresh.Token, "a new password"); !errors.Is(err, ErrInviteNotNow) {
		t.Errorf("the new-password acceptance into a suspended business answered %v, want ErrInviteNotNow", err)
	}
	if n := w.rowsOf(w.tenant, w.email) + w.rowsOf(w.tenant, stranger); n != 0 {
		t.Errorf("%d rows written into a suspended business", n)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities WHERE email = $1`, stranger); n != 0 {
		t.Error("an identity was born for an invitation into a suspended business")
	}
	if w.inviteState(withSession.Invite.ID) != "open" || w.inviteState(fresh.Invite.ID) != "open" {
		t.Error("an invitation into a suspended business was spent")
	}
	w.exec(`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, w.tenant)
	if _, err := w.s.AcceptWithSession(context.Background(), withSession.Token, session); err != nil {
		t.Errorf("CONTROL: after the suspension the link does not work: %v", err)
	}
}

// TestAdminInvites_TheMembershipCarriesTheInvitationsSpelling (D1/D4): the invited address as the owner
// typed it is what the new membership row stores, on both paths -- the identity's own spelling never
// reaches this business's rows (M14-16's list would show the person's other spelling).
func TestAdminInvites_TheMembershipCarriesTheInvitationsSpelling(t *testing.T) {
	owner := ownerConn(t)
	w := newInviteWorld(t, owner, true)
	a := w.business("M14-15 A")
	rowA := w.linked(a, "active", "backfill", 48*time.Hour)
	typed := strings.ToUpper(w.email[:1]) + w.email[1:]
	is := w.issue(w.tenant, w.ownerRow, typed)
	got, err := w.s.AcceptWithSession(context.Background(), is.Token, w.session(a, rowA))
	if err != nil {
		t.Fatalf("AcceptWithSession: %v", err)
	}
	var stored string
	if err := owner.QueryRow(context.Background(), `SELECT email::text FROM admin_users WHERE id = $1`, got.AdminUserID).Scan(&stored); err != nil || stored != typed {
		t.Errorf("the membership stores %q (%v), want the invitation's %q", stored, err, typed)
	}
}

// TestAdminInvites_AHoldingMembershipBirthsNoInvitation (ADR 0031 §5.11: "holding üyeliği davet
// doğurmaz"): nothing but the owner's send writes admin_invites and nothing but the three functions of
// 00077 reads it -- no function body in the database (the holding definers' op_* and holding_* among
// them) names the table, and no store query outside admininvites.sql does. So a business joining a
// holding, or a holding member acting, can never create an invitation or a membership through one.
// CONTROL (in the scan): a holding-shaped function that inserts an invitation is named.
func TestAdminInvites_AHoldingMembershipBirthsNoInvitation(t *testing.T) {
	owner := ownerConn(t)
	allowed := []string{"identity_accept_invite", "identity_accept_invite_new", "resolve_admin_invite_by_token_hash"}
	scan := func(q interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}) []string {
		rows, err := q.Query(context.Background(), `SELECT p.proname FROM pg_proc p
		                                              WHERE p.pronamespace = 'public'::regnamespace AND p.prosrc ~* 'admin_invites'
		                                              ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	got := scan(owner)
	if !slices.Equal(got, allowed) {
		t.Errorf("the functions naming admin_invites are %v, want exactly %v", got, allowed)
	}
	tx, err := owner.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil {
			t.Errorf("roll the control back: %v", err)
		}
	}()
	if _, err := tx.Exec(context.Background(), `CREATE FUNCTION public.zz_holding_invites() RETURNS void LANGUAGE sql AS 'INSERT INTO public.admin_invites (tenant_id) VALUES (NULL)'`); err != nil {
		t.Fatal(err)
	}
	if c := scan(tx); slices.Equal(c, allowed) {
		t.Error("CONTROL: a holding-shaped writer of admin_invites is not named")
	}
	entries, err := os.ReadDir(filepath.Join("..", "..", "db", "queries"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("..", "..", "db", "queries", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() != "admininvites.sql" && strings.Contains(string(b), "admin_invites") {
			t.Errorf("db/queries/%s names admin_invites", e.Name())
		}
	}
}
