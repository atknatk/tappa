package adminauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// identityops_db_test.go -- M14-08 against real Postgres: Switch, RevokeAll and the dual-write of a
// password (ChangeOwnPassword through the identity, the legacy reset link of a linked row). The
// identities are committed through newIDWorld (identity_db_test.go: the owner connection holds the
// operator-tables lock shared and takes the identity back at the end).
//
// "FLAG ON" AND "FLAG OFF" ARE THE TWO SIGN-INS. Until M14-09 wires the login handler the flag picks
// between AuthenticatePerson (the identity path, on) and Authenticate + Issue (today's, off); both
// are this package's, and a password changed under one must sign in under the other (the card's
// dual-write criterion). The writes themselves do not read the flag.

// cheapManager is testManager with the password change's hashing at bcrypt.MinCost (hashFn).
func cheapManager(t *testing.T, m *Manager) *Manager {
	t.Helper()
	m.hashFn = func(p string) (string, error) {
		h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.MinCost)
		return string(h), err
	}
	return m
}

// signInOn is a sign-in with the flag on: the identity's session (AuthenticatePerson), landing in want.
func (w *idWorld) signInOn(m *Manager, password string, want uuid.UUID) (Issued, error) {
	w.t.Helper()
	in, err := m.AuthenticatePerson(context.Background(), w.email, password, want, generousBudget())
	if err != nil {
		return Issued{}, err
	}
	if in.Session == nil {
		w.t.Fatalf("AuthenticatePerson verified legacy rows only (%+v); this world's rows are all linked", in.Legacy.Verified())
	}
	return *in.Session, nil
}

// signInOff is a sign-in with the flag off: today's Authenticate, then Issue in tenant.
func (w *idWorld) signInOff(m *Manager, password string, tenant uuid.UUID) (Issued, []Verified, error) {
	w.t.Helper()
	auth, err := m.Authenticate(context.Background(), w.email, password)
	if err != nil {
		return Issued{}, nil, err
	}
	for _, v := range auth.Verified() {
		if v.TenantID == tenant {
			is, err := m.Issue(context.Background(), v)
			return is, auth.Verified(), err
		}
	}
	w.t.Fatalf("the legacy sign-in did not verify the row in %s (%d verified)", tenant, len(auth.Verified()))
	return Issued{}, nil, nil
}

// digestInvariant is ADR 0031 §5.1's tested invariant: every linked, unrefused row of this identity
// carries the identity's digest, byte for byte.
func (w *idWorld) digestInvariant() {
	w.t.Helper()
	if n := w.int(`SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                WHERE i.id = $1 AND a.unlinked_at IS NULL AND a.password_hash <> i.password_hash`, w.ident); n != 0 {
		w.t.Fatalf("%d linked row(s) whose digest is not the identity's (ADR 0031 §5.1)", n)
	}
}

// TestSwitch_ANewTokenAndTheOldOneResolvesNothing (ADR 0031 §5.4): a switch hands back a NEW token
// for the target business; the old token no longer resolves (Verify -- unchanged by M14-08 -- says
// ErrNoSession), the new one resolves to the target membership. Every refusal is ErrSwitchRefused:
// the current business, a business the person is not a member of, an already-switched (revoked)
// session, a disabled target membership, and an unlinked row's session.
func TestSwitch_ANewTokenAndTheOldOneResolvesNothing(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb, tc, td := w.tenant("M14-08 Switch A"), w.tenant("M14-08 Switch B"), w.tenant("M14-08 Switch C"), w.tenant("M14-08 Switch D")
	w.linked(ta, "active", "backfill", 72*time.Hour)
	b := w.linked(tb, "active", "backfill", 48*time.Hour)
	w.linked(td, "disabled", "backfill", 24*time.Hour)
	ctx := context.Background()

	in, err := w.signInOn(m, w.pw, ta)
	if err != nil || in.Session.TenantID != ta {
		t.Fatalf("sign-in: %+v, %v", in.Session, err)
	}
	sw, err := m.Switch(ctx, in.Token, tb)
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if sw.Session.TenantID != tb || sw.Session.AdminUserID != b || sw.Session.ID == in.Session.ID {
		t.Fatalf("the switch landed %+v, want a new session of %s in %s", sw.Session, b, tb)
	}
	if sw.Token.String() == "" || sw.Token.reveal() == in.Token.reveal() {
		t.Fatal("the switch did not hand back a new token")
	}
	if _, err := m.Verify(ctx, in.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("the old token after the switch: %v, want ErrNoSession", err)
	}
	r, err := m.Verify(ctx, sw.Token)
	if err != nil || r.TenantID != tb || r.AdminUserID != b || r.SessionID != sw.Session.ID {
		t.Fatalf("the new token: %+v, %v", r, err)
	}

	for _, c := range []struct {
		name   string
		token  Token
		target uuid.UUID
	}{
		{"the current business", sw.Token, tb},
		{"a business without a membership", sw.Token, tc},
		{"a disabled membership", sw.Token, td},
		{"a session already switched away", in.Token, ta},
		{"no target", sw.Token, uuid.Nil},
	} {
		if _, err := m.Switch(ctx, c.token, c.target); !errors.Is(err, ErrSwitchRefused) {
			t.Errorf("%s: %v, want ErrSwitchRefused", c.name, err)
		}
	}

	// An unlinked row's session (today's sign-in) has no identity to switch through.
	plain := newIDWorld(t, w.owner, false)
	tp := plain.tenant("M14-08 Switch plain")
	plain.row(tp, plain.digest, "active", time.Hour)
	legacy, _, err := plain.signInOff(m, plain.pw, tp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Switch(ctx, legacy.Token, ta); !errors.Is(err, ErrSwitchRefused) {
		t.Errorf("an unlinked row's session: %v, want ErrSwitchRefused", err)
	}
	if _, err := m.Verify(ctx, legacy.Token); err != nil {
		t.Errorf("a refused switch touched the unlinked session: %v", err)
	}
}

// TestSwitch_ConcurrentSwitchesOfOneSessionHaveOneWinner (ADR 0031 §5.4, bulgu 29): N switches of one
// session at once -- exactly one new session, every other caller ErrSwitchRefused (and so no cookie
// to write). Run under -race.
func TestSwitch_ConcurrentSwitchesOfOneSessionHaveOneWinner(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb := w.tenant("M14-08 Race A"), w.tenant("M14-08 Race B")
	w.linked(ta, "active", "backfill", 72*time.Hour)
	w.linked(tb, "active", "backfill", 48*time.Hour)
	in, err := w.signInOn(m, w.pw, ta)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		won     int
		refused int
		other   []error
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Switch(context.Background(), in.Token, tb)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrSwitchRefused):
				refused++
			default:
				other = append(other, err)
			}
		}()
	}
	wg.Wait()
	if won != 1 || refused != n-1 || len(other) != 0 {
		t.Fatalf("%d won, %d refused, other errors %v; want 1 and %d", won, refused, other, n-1)
	}
	if live := w.sessions(tb, true); live != 1 {
		t.Fatalf("%d live sessions in the target, want exactly the winner's", live)
	}
}

// TestRevokeAll_ThePersonEverywhereOrTheRowAlone: a linked row's session revokes the person's other
// sessions in EVERY business (keepCurrent: its own survives) and reports ThroughIdentity -- the
// database wrote each business its sign-out rows; an unlinked row's session revokes that admin's
// sessions in its own business only and hands back their ids for the caller's rows.
func TestRevokeAll_ThePersonEverywhereOrTheRowAlone(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb := w.tenant("M14-08 Revoke A"), w.tenant("M14-08 Revoke B")
	a := w.linked(ta, "active", "backfill", 72*time.Hour)
	w.linked(tb, "active", "backfill", 48*time.Hour)
	ctx := context.Background()

	cur, err := w.signInOn(m, w.pw, ta)
	if err != nil {
		t.Fatal(err)
	}
	other, err := w.signInOn(m, w.pw, ta)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := w.signInOn(m, w.pw, tb)
	if err != nil {
		t.Fatal(err)
	}
	logoutsB := w.audit(tb, "admin.logout")
	rv, err := m.RevokeAll(ctx, ta, a, cur.Session.ID, cur.Token, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rv.ThroughIdentity || rv.Revoked != 2 || len(rv.Sessions) != 0 {
		t.Fatalf("RevokeAll = %+v, want 2 through the identity", rv)
	}
	if _, err := m.Verify(ctx, cur.Token); err != nil {
		t.Errorf("the current session was revoked with keepCurrent: %v", err)
	}
	for name, tok := range map[string]Token{"the other session here": other.Token, "the session in the other business": elsewhere.Token} {
		if _, err := m.Verify(ctx, tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s survived: %v", name, err)
		}
	}
	if got := w.audit(tb, "admin.logout") - logoutsB; got != 1 {
		t.Errorf("the other business got %d sign-out rows, want its one", got)
	}

	plain := newIDWorld(t, w.owner, false)
	tp := plain.tenant("M14-08 Revoke plain")
	pa := plain.row(tp, plain.digest, "active", time.Hour)
	s1, _, err := plain.signInOff(m, plain.pw, tp)
	if err != nil {
		t.Fatal(err)
	}
	s2, _, err := plain.signInOff(m, plain.pw, tp)
	if err != nil {
		t.Fatal(err)
	}
	rv, err = m.RevokeAll(ctx, tp, pa, s1.Session.ID, s1.Token, false)
	if err != nil {
		t.Fatal(err)
	}
	if rv.ThroughIdentity || rv.Revoked != 2 || len(rv.Sessions) != 2 {
		t.Fatalf("an unlinked row's RevokeAll = %+v, want its 2 sessions, not through the identity", rv)
	}
	for _, tok := range []Token{s1.Token, s2.Token} {
		if _, err := m.Verify(ctx, tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("an unlinked row's session survived RevokeAll(false): %v", err)
		}
	}
}

// TestChangeOwnPassword_ThroughTheIdentitySignsInUnderEitherFlag is the card's dual-write criterion:
//
//	ON -> OFF   signed in through the identity, the password is changed: today's sign-in (flag off)
//	            verifies the NEW password on every linked row, the old one on none;
//	OFF -> ON   signed in today's way on a linked row, the password is changed (through the identity
//	            again: the row refuses a direct write): the identity sign-in takes the newest.
//
// Each change reports Recorded (the database wrote each business its admin.password_changed row --
// exactly one in the business the change was made in), revokes the person's other sessions in every
// business and keeps the one that made it; the digest invariant holds after each.
func TestChangeOwnPassword_ThroughTheIdentitySignsInUnderEitherFlag(t *testing.T) {
	d := testDB(t)
	m := cheapManager(t, testManager(t, d))
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb := w.tenant("M14-08 Password A"), w.tenant("M14-08 Password B")
	a := w.linked(ta, "active", "backfill", 72*time.Hour)
	b := w.linked(tb, "active", "backfill", 48*time.Hour)
	ctx := context.Background()
	second, third := "second-"+uuid.NewString(), "third-"+uuid.NewString()

	// ON -> OFF.
	on, err := w.signInOn(m, w.pw, ta)
	if err != nil {
		t.Fatal(err)
	}
	spare, err := w.signInOn(m, w.pw, tb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ChangeOwnPassword(ctx, ta, a, on.Session.ID, on.Token, "not-the-password", second); !errors.Is(err, ErrWrongCurrentPassword) {
		t.Fatalf("a wrong current password through the identity: %v", err)
	}
	if _, err := m.ChangeOwnPassword(ctx, ta, a, on.Session.ID, on.Token, w.pw, w.pw); !errors.Is(err, ErrSameAsCurrentPassword) {
		t.Fatalf("the same password through the identity: %v", err)
	}
	changed := w.audit(ta, "admin.password_changed")
	ch, err := m.ChangeOwnPassword(ctx, ta, a, on.Session.ID, on.Token, w.pw, second)
	if err != nil || !ch.Recorded {
		t.Fatalf("ChangeOwnPassword through the identity = %+v, %v", ch, err)
	}
	if got := w.audit(ta, "admin.password_changed") - changed; got != 1 {
		t.Errorf("%d admin.password_changed rows in the business, want the database's one", got)
	}
	if _, err := m.Verify(ctx, on.Token); err != nil {
		t.Errorf("the session that made the change was revoked: %v", err)
	}
	if _, err := m.Verify(ctx, spare.Token); !errors.Is(err, ErrNoSession) {
		t.Errorf("the person's session in the other business survived the change: %v", err)
	}
	w.digestInvariant()
	off, verified, err := w.signInOff(m, second, tb)
	if err != nil || len(verified) != 2 {
		t.Fatalf("flag off, the new password: %d rows verified, %v; want both linked rows", len(verified), err)
	}
	if _, err := m.Authenticate(ctx, w.email, w.pw); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("flag off, the old password: %v, want ErrBadCredentials", err)
	}

	// OFF -> ON: off is a session of today's kind, on the linked row b.
	ch, err = m.ChangeOwnPassword(ctx, tb, b, off.Session.ID, off.Token, second, third)
	if err != nil || !ch.Recorded {
		t.Fatalf("ChangeOwnPassword from a today's-kind session on a linked row = %+v, %v", ch, err)
	}
	w.digestInvariant()
	if _, err := w.signInOn(m, third, uuid.Nil); err != nil {
		t.Fatalf("flag on, the newest password: %v", err)
	}
	if in, err := m.AuthenticatePerson(ctx, w.email, second, uuid.Nil, generousBudget()); !errors.Is(err, ErrBadCredentials) || in.Session != nil {
		t.Fatalf("flag on, the previous password: %+v, %v", in.Session, err)
	}
}

// TestConsume_ALinkedRowsLinkWritesTheIdentity (ADR 0031 §5.9; ADR 0015's meaning change starts
// here): a per-row link of a LINKED row is spent through identity_consume_row_reset -- Recorded (the
// database wrote the trail), every linked row and the identity take the new digest (both sign-ins
// verify it), every session of the person is revoked, and the mailbox is marked proved. An unlinked
// row's link is today's consumption (Recorded false).
func TestConsume_ALinkedRowsLinkWritesTheIdentity(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	r := cheapResets(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb := w.tenant("M14-08 Reset A"), w.tenant("M14-08 Reset B")
	a := w.linked(ta, "active", "backfill", 72*time.Hour)
	w.linked(tb, "active", "backfill", 48*time.Hour)
	ctx := context.Background()

	s, err := w.signInOn(m, w.pw, tb)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := r.issue(ctx, ta, a)
	if err != nil {
		t.Fatal(err)
	}
	completed := w.audit(ta, "admin.recovery.completed")
	next := "after-reset-" + uuid.NewString()
	c, _, err := r.Consume(ctx, issued.Token, next)
	if err != nil || !c.Recorded || c.TenantID != ta || c.AdminUserID != a {
		t.Fatalf("Consume of a linked row's link = %+v, %v", c, err)
	}
	if got := w.audit(ta, "admin.recovery.completed") - completed; got != 1 {
		t.Errorf("%d admin.recovery.completed rows in the link's business, want the database's one", got)
	}
	if _, err := m.Verify(ctx, s.Token); !errors.Is(err, ErrNoSession) {
		t.Errorf("the person's session in the other business survived the reset: %v", err)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at IS NOT NULL AND confirm_scope = 'reset'`, w.ident); n != 1 {
		t.Error("the identity's mailbox is not marked proved, or no reset-scope window opened")
	}
	w.digestInvariant()
	if _, err := w.signInOn(m, next, uuid.Nil); err != nil {
		t.Errorf("flag on after the reset: %v", err)
	}
	if _, verified, err := w.signInOff(m, next, tb); err != nil || len(verified) != 2 {
		t.Errorf("flag off after the reset: %d verified, %v", len(verified), err)
	}
	if _, _, err := r.Consume(ctx, issued.Token, "again-"+uuid.NewString()); !errors.Is(err, ErrResetUnusable) {
		t.Errorf("the spent link again: %v, want ErrResetUnusable", err)
	}

	plain := newIDWorld(t, w.owner, false)
	tp := plain.tenant("M14-08 Reset plain")
	pa := plain.row(tp, plain.digest, "active", time.Hour)
	pl, err := r.issue(ctx, tp, pa)
	if err != nil {
		t.Fatal(err)
	}
	if c, _, err := r.Consume(ctx, pl.Token, "plain-"+uuid.NewString()); err != nil || c.Recorded {
		t.Fatalf("an unlinked row's link = %+v, %v; want today's consumption", c, err)
	}
}
