package adminauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// identityaccount_db_test.go -- M14-18 against real Postgres: the account page's identity acts end to
// end through this package (the e-mail verification, the window's read, Keep all, unlink and Undo,
// "Link a business" in both directions). Fixtures are committed through newIDWorld
// (identity_db_test.go).

// verifiedOn signs w in through the identity (flag on) in tenant and returns the session's Resolved
// -- the value the handler hands every act -- and its token.
func (w *idWorld) verifiedOn(m *Manager, tenant uuid.UUID) (Resolved, Token) {
	w.t.Helper()
	is, err := w.signInOn(m, w.pw, tenant)
	if err != nil {
		w.t.Fatalf("sign in: %v", err)
	}
	r, err := m.Verify(context.Background(), is.Token)
	if err != nil {
		w.t.Fatalf("Verify: %v", err)
	}
	return r, is.Token
}

// legacyOn signs an UNLINKED row in the way the flag-off sign-in does (Authenticate + Issue) and
// returns its Resolved and token.
func (w *idWorld) legacyOn(m *Manager, password string, tenant uuid.UUID) (Resolved, Token) {
	w.t.Helper()
	is, _, err := w.signInOff(m, password, tenant)
	if err != nil {
		w.t.Fatalf("legacy sign in: %v", err)
	}
	r, err := m.Verify(context.Background(), is.Token)
	if err != nil {
		w.t.Fatalf("Verify: %v", err)
	}
	return r, is.Token
}

// age makes a session as old as the panel's usual one: authenticated d ago (as the owner).
func (w *idWorld) age(s Resolved, d time.Duration) {
	w.t.Helper()
	w.exec(`UPDATE admin_sessions SET authenticated_at = now() - $2::interval WHERE id = $1`, s.SessionID, d.String())
}

// TestVerification_OnlyTheSameIdentitysBrowserVerifiesAndASwitchDoesNotBreakIt is R2 / R19 / S16:
// the link a linked session asked for verifies NOTHING from a browser holding another identity's
// session, an unlinked row's session or a revoked one (ErrVerificationRefused, the address still
// unverified, the link unspent); from the SAME identity's session in ANOTHER business -- the person
// switched between the request and the click -- it verifies, opens the first_verify window, and is
// spent (a second press is refused). A malformed value is the same refusal.
func TestVerification_OnlyTheSameIdentitysBrowserVerifiesAndASwitchDoesNotBreakIt(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	owner := ownerConn(t)
	w := newIDWorld(t, owner, true)
	// The link this test writes goes before the identity does (newIDWorld's cleanup runs after this one).
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `DELETE FROM admin_identity_verifications WHERE identity_id = $1`, w.ident); err != nil {
			t.Errorf("cleanup the verification link: %v", err)
		}
	})
	ta, tb := w.tenant("M14-18 Verify A"), w.tenant("M14-18 Verify B")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	w.linked(tb, "active", "backfill", 48*time.Hour)
	ctx := context.Background()

	ra, tokA := w.verifiedOn(m, ta)
	issued, err := m.RequestVerification(ctx, ra, tokA)
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	if issued.Recipient != w.email || issued.ID == uuid.Nil || !issued.ExpiresAt.After(time.Now().Add(23*time.Hour)) {
		t.Fatalf("issued %+v: want the identity's address and a day's link", issued)
	}
	raw := issued.token.reveal()

	other := newIDWorld(t, owner, true)
	to := other.tenant("M14-18 Verify Other")
	other.linked(to, "active", "backfill", 48*time.Hour)
	_, tokOther := other.verifiedOn(m, to)
	plain := newIDWorld(t, owner, false)
	tp := plain.tenant("M14-18 Verify Plain")
	plain.row(tp, plain.digest, "active", time.Hour)
	_, tokPlain := plain.legacyOn(m, plain.pw, tp)
	_, tokRevoked := w.verifiedOn(m, tb)
	revoked, _ := m.Verify(ctx, tokRevoked)
	if err := m.Revoke(ctx, revoked.TenantID, revoked.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		raw   string
		token Token
	}{
		{"another identity's browser", raw, tokOther},
		{"an unlinked row's browser", raw, tokPlain},
		{"a revoked session", raw, tokRevoked},
		{"a malformed link", "not-a-token", tokA},
	} {
		if _, err := m.VerifyEmail(ctx, c.raw, c.token); !errors.Is(err, ErrVerificationRefused) {
			t.Errorf("%s: %v, want ErrVerificationRefused", c.name, err)
		}
	}
	if n := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at IS NULL AND confirm_scope IS NULL`, w.ident); n != 1 {
		t.Fatal("a refused click verified the address or opened a window")
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_verifications WHERE id = $1 AND used_at IS NULL`, issued.ID); n != 1 {
		t.Fatal("a refused click spent the link")
	}

	// The person switches to B between the request and the click: a new session, the same identity.
	moved, err := m.Switch(ctx, tokA, tb)
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	first, err := m.VerifyEmail(ctx, raw, moved.Token)
	if err != nil || !first {
		t.Fatalf("the same identity's session in another business: %v, %v; want verified, first", first, err)
	}
	if n := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at IS NOT NULL AND confirm_scope = 'first_verify'`, w.ident); n != 1 {
		t.Fatal("the click did not verify the address and open the first_verify window")
	}
	if _, err := m.VerifyEmail(ctx, raw, moved.Token); !errors.Is(err, ErrVerificationRefused) {
		t.Errorf("a spent link: %v, want ErrVerificationRefused", err)
	}
	rb, _ := m.Verify(ctx, moved.Token)
	if _, err := m.RequestVerification(ctx, rb, moved.Token); !errors.Is(err, ErrAlreadyVerified) {
		t.Errorf("a verified address asked again: %v, want ErrAlreadyVerified", err)
	}
	if _, err := m.RequestVerification(ctx, plain.mustVerify(m, tokPlain), tokPlain); !errors.Is(err, ErrNotLinked) {
		t.Errorf("an unlinked row asked for a link: %v, want ErrNotLinked", err)
	}
	if n := w.events("email_verified"); n != 1 {
		t.Errorf("%d email_verified events, want 1", n)
	}
}

func (w *idWorld) mustVerify(m *Manager, tok Token) Resolved {
	w.t.Helper()
	r, err := m.Verify(context.Background(), tok)
	if err != nil {
		w.t.Fatalf("Verify: %v", err)
	}
	return r
}

// TestWindow_ReadKeepUnlinkAndUndo is the window's acts through this package (ADR 0031 §5.8): the
// read lists the reset scope's links; an unlink with the WRONG password changes nothing and records
// the identity's login_failed event; with the right one -- on a session six minutes old, so only the
// fresh session this package opens can satisfy identity_unlink_backfilled -- the row is refused, the
// fresh session is revoked again (and handed back for its sign-out row), the person's cookie session
// is untouched; the refused row is listed as refused, and its Undo takes it back; Keep all closes the
// window (the read's list is gone, a further unlink is ErrWindowRefused).
func TestWindow_ReadKeepUnlinkAndUndo(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb := w.tenant("M14-18 Window A"), w.tenant("M14-18 Window B")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	rowB := w.linked(tb, "active", "password", 48*time.Hour)
	w.exec(`UPDATE admin_identities SET email_verified_at = now(), confirm_opened_at = now(), confirm_open_until = now() + interval '7 days',
	        confirm_scope = 'reset' WHERE id = $1`, w.ident)
	ctx := context.Background()
	ra, tok := w.verifiedOn(m, ta)
	w.age(ra, 6*time.Minute)

	win, err := m.ConfirmWindow(ctx, tok)
	if err != nil || !win.Linked || !win.Open || win.Scope != "reset" || len(win.Rows) != 2 {
		t.Fatalf("ConfirmWindow = %+v, %v; want the reset window and both businesses", win, err)
	}

	if _, _, err := m.Unlink(ctx, ra, "not-the-password", tb, generousBudget()); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("unlink with a wrong password: %v", err)
	}
	if id, _ := w.link(rowB); id != w.ident {
		t.Fatal("a wrong password unlinked the row")
	}
	if n := w.events("login_failed"); n != 1 {
		t.Errorf("%d login_failed events after a wrong password, want 1", n)
	}

	act, ended, err := m.Unlink(ctx, ra, w.pw, tb, generousBudget())
	if err != nil || ended {
		t.Fatalf("Unlink: %v, ended %v", err, ended)
	}
	if id, _ := w.link(rowB); id != uuid.Nil {
		t.Fatal("the row is still linked")
	}
	if len(act.Temporary) != 1 || !act.Revoked[0] {
		t.Fatalf("fresh sessions %+v: want one, revoked", act)
	}
	if n := w.int(`SELECT count(*) FROM admin_sessions WHERE id = $1 AND revoked_at IS NULL`, act.Temporary[0].ID); n != 0 {
		t.Fatal("the fresh session is still live")
	}
	if _, err := m.Verify(ctx, tok); err != nil {
		t.Fatalf("the person's own session died with the unlink of another business: %v", err)
	}
	win, _ = m.ConfirmWindow(ctx, tok)
	refused := 0
	for _, r := range win.Rows {
		if r.Refused && r.TenantID == tb {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("the refused row is not listed as refused: %+v", win.Rows)
	}

	if _, err := m.Relink(ctx, ra, w.pw, tb, generousBudget()); err != nil {
		t.Fatalf("Relink: %v", err)
	}
	if id, via := w.link(rowB); id != w.ident || via != "account" {
		t.Fatalf("after the Undo the row is %s/%q", id, via)
	}
	w.digestInvariant()

	if err := m.KeepAll(ctx, tok); err != nil {
		t.Fatalf("KeepAll on a stale (live) session: %v", err)
	}
	if win, _ := m.ConfirmWindow(ctx, tok); win.Open || len(win.Rows) != 0 || !win.Verified {
		t.Fatalf("after Keep all: %+v", win)
	}
	if _, _, err := m.Unlink(ctx, ra, w.pw, tb, generousBudget()); !errors.Is(err, ErrWindowRefused) {
		t.Errorf("an unlink after Keep all: %v, want ErrWindowRefused", err)
	}
	if err := m.KeepAll(ctx, tok); !errors.Is(err, ErrWindowRefused) {
		t.Errorf("a second Keep all: %v, want ErrWindowRefused", err)
	}
}

// TestUnlink_OfTheCurrentBusinessEndsTheSession: unlinking the business the person is in revokes its
// sessions, the cookie's among them -- the handler then clears the cookie.
func TestUnlink_OfTheCurrentBusinessEndsTheSession(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tb := w.tenant("M14-18 Current A"), w.tenant("M14-18 Current B")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	w.linked(tb, "active", "backfill", 48*time.Hour)
	w.exec(`UPDATE admin_identities SET email_verified_at = now(), confirm_opened_at = now(), confirm_open_until = now() + interval '7 days',
	        confirm_scope = 'first_verify' WHERE id = $1`, w.ident)
	ra, tok := w.verifiedOn(m, ta)
	if _, ended, err := m.Unlink(context.Background(), ra, w.pw, ta, generousBudget()); err != nil || !ended {
		t.Fatalf("Unlink of the current business: ended %v, %v", ended, err)
	}
	if _, err := m.Verify(context.Background(), tok); !errors.Is(err, ErrNoSession) {
		t.Errorf("the cookie's session after its business was unlinked: %v, want ErrNoSession", err)
	}
}

// TestLinkBusiness_ASixMinuteOldSessionLinksWithThePasswordTypedAgain is S6: the identity session is
// six minutes old (identity_link_legacy wants five), the person types its password again and the old
// business's -- the row is linked, its proof session and the fresh identity session are revoked, the
// person's own session lives on. A wrong OLD password links nothing and is the same nil-error answer
// (the one result); a wrong CURRENT password is ErrBadCredentials; a refused row is no candidate.
func TestLinkBusiness_ASixMinuteOldSessionLinksWithThePasswordTypedAgain(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tl, tr := w.tenant("M14-18 Link A"), w.tenant("M14-18 Link Legacy"), w.tenant("M14-18 Link Refused")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	oldPW := "old-" + uuid.NewString()
	legacy := w.row(tl, fixtureDigest(t, oldPW), "active", 24*time.Hour)
	refused := w.row(tr, fixtureDigest(t, oldPW), "active", 24*time.Hour)
	w.exec(`UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
	ctx := context.Background()
	ra, tok := w.verifiedOn(m, ta)
	w.age(ra, 6*time.Minute)

	if _, err := m.LinkBusiness(ctx, ra, "not-the-password", oldPW, generousBudget()); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("a wrong current password: %v, want ErrBadCredentials", err)
	}
	res, err := m.LinkBusiness(ctx, ra, w.pw, "not-the-old-one", generousBudget())
	if err != nil || res.Linked != 0 {
		t.Fatalf("a wrong old password: %+v, %v; want the one result (nothing, nil)", res, err)
	}
	if id, _ := w.link(legacy); id != uuid.Nil {
		t.Fatal("a wrong old password linked the row")
	}
	res, err = m.LinkBusiness(ctx, ra, w.pw, oldPW, generousBudget())
	if err != nil || res.Linked != 1 {
		t.Fatalf("LinkBusiness on a six-minute-old session = %+v, %v; want one row linked", res, err)
	}
	if id, via := w.link(legacy); id != w.ident || via != "password" {
		t.Fatalf("the legacy row is %s/%q, want linked", id, via)
	}
	if id, _ := w.link(refused); id != uuid.Nil {
		t.Fatal("the refused row was linked (it is no candidate)")
	}
	for i, s := range res.Temporary {
		if !res.Revoked[i] || w.int(`SELECT count(*) FROM admin_sessions WHERE id = $1 AND revoked_at IS NULL`, s.ID) != 0 {
			t.Errorf("fresh session %s still live", s.ID)
		}
	}
	if n := w.int(`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND revoked_at IS NULL`, tl); n != 0 {
		t.Errorf("%d live sessions left in the linked business (the proof session must be gone)", n)
	}
	if _, err := m.Verify(ctx, tok); err != nil {
		t.Fatalf("the person's own session: %v", err)
	}
	w.digestInvariant()
}

// TestLinkBusiness_FromAnUnlinkedSessionLinksThisRow is the reverse direction (the card's last
// criterion; M14-13's Choose paragraph sends an unlinked person to the account page): an older
// per-business sign-in -- its row UNLINKED, its session old -- types its own password and the
// identity's; this row is linked to the identity, the person's session lives on and now resolves
// linked; no session opened for the proof stays live. A wrong identity password and an address with
// no identity are the one nil-error answer; a wrong own password is ErrBadCredentials.
func TestLinkBusiness_FromAnUnlinkedSessionLinksThisRow(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	ta, tl := w.tenant("M14-18 Reverse A"), w.tenant("M14-18 Reverse Legacy")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	rowPW := "row-" + uuid.NewString()
	legacy := w.row(tl, fixtureDigest(t, rowPW), "active", 24*time.Hour)
	ctx := context.Background()
	rl, tok := w.legacyOn(m, rowPW, tl)
	if rl.Linked {
		t.Fatal("PREMISE: the legacy session resolves linked")
	}
	w.age(rl, 6*time.Minute)

	if _, err := m.LinkBusiness(ctx, rl, "not-this-rows", w.pw, generousBudget()); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("a wrong own password: %v, want ErrBadCredentials", err)
	}
	if res, err := m.LinkBusiness(ctx, rl, rowPW, "not-the-identitys", generousBudget()); err != nil || res.Linked != 0 {
		t.Fatalf("a wrong identity password: %+v, %v; want the one result", res, err)
	}
	if n := w.int(`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND revoked_at IS NULL`, tl); n != 1 {
		t.Fatalf("%d live sessions in the legacy business after a refused link, want the person's one (the proof revoked)", n)
	}
	res, err := m.LinkBusiness(ctx, rl, rowPW, w.pw, generousBudget())
	if err != nil || res.Linked != 1 {
		t.Fatalf("the reverse link = %+v, %v; want this row linked", res, err)
	}
	if id, _ := w.link(legacy); id != w.ident {
		t.Fatal("the row is not linked to the identity")
	}
	now, err := m.Verify(ctx, tok)
	if err != nil || !now.Linked {
		t.Fatalf("the person's own session after the link: %+v, %v; want live and linked", now, err)
	}
	if n := w.int(`SELECT count(*) FROM admin_sessions WHERE tenant_id = $1 AND revoked_at IS NULL`, tl); n != 1 {
		t.Errorf("%d live sessions in the linked business, want the person's one", n)
	}
	for i, s := range res.Temporary {
		if !res.Revoked[i] || w.int(`SELECT count(*) FROM admin_sessions WHERE id = $1 AND revoked_at IS NULL`, s.ID) != 0 {
			t.Errorf("the fresh identity session %s is still live", s.ID)
		}
	}
	w.digestInvariant()

	nobody := newIDWorld(t, w.owner, false)
	tn := nobody.tenant("M14-18 Reverse Nobody")
	nobody.row(tn, nobody.digest, "active", time.Hour)
	rn, _ := nobody.legacyOn(m, nobody.pw, tn)
	if res, err := m.LinkBusiness(ctx, rn, nobody.pw, "anything", generousBudget()); err != nil || res.Linked != 0 {
		t.Errorf("an address with no identity: %+v, %v; want the one result", res, err)
	}
}

// TestAccountActs_TheRowsAddressMustNameTheSessionsOwnIdentity: the acts find the identity by the
// session row's address and hold it to the SESSION's person (the bucket): a row linked to X that
// carries Y's address (a shape no writer makes -- the defence layer behind "linked rows share the
// identity's address") is refused, with Y's password as with X's, and asks for no link -- Y's
// identity is never acted on through X's session.
func TestAccountActs_TheRowsAddressMustNameTheSessionsOwnIdentity(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	owner := ownerConn(t)
	x, y := newIDWorld(t, owner, true), newIDWorld(t, owner, true)
	ta, ty := x.tenant("M14-18 Mismatch A"), y.tenant("M14-18 Mismatch Y")
	x.linked(x.tenant("M14-18 Mismatch X"), "active", "backfill", 48*time.Hour)
	y.linked(ty, "active", "backfill", 48*time.Hour)
	// A's row: Y's address and X's digest, linked to X (the owner writes it before the link).
	odd := y.row(ta, x.digest, "active", time.Hour)
	x.exec(`UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() - interval '3 days' WHERE id = $1`, odd, x.ident)
	x.exec(`UPDATE admin_identities SET email_verified_at = now(), confirm_opened_at = now(), confirm_open_until = now() + interval '7 days',
	        confirm_scope = 'reset' WHERE id = $1`, y.ident)
	ra, tok := x.verifiedOn(m, ta)
	if ra.AdminUserID != odd {
		t.Fatalf("PREMISE: the session landed on %s, want the odd row", ra.AdminUserID)
	}
	ctx := context.Background()
	for _, pw := range []string{y.pw, x.pw} {
		if _, _, err := m.Unlink(ctx, ra, pw, ty, generousBudget()); !errors.Is(err, ErrNotLinked) {
			t.Errorf("unlink through a row naming another identity: %v, want ErrNotLinked", err)
		}
		if _, err := m.LinkBusiness(ctx, ra, pw, "x", generousBudget()); !errors.Is(err, ErrNotLinked) {
			t.Errorf("link through a row naming another identity: %v, want ErrNotLinked", err)
		}
	}
	if _, err := m.RequestVerification(ctx, ra, tok); !errors.Is(err, ErrNotLinked) {
		t.Errorf("a verification through a row naming another identity: %v, want ErrNotLinked", err)
	}
	if id, _ := y.link(y.int2(ty)); id != y.ident {
		t.Error("Y's membership was touched through X's session")
	}
}

// int2 is the admin row of this world's address in tenant.
func (w *idWorld) int2(tenant uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(context.Background(), `SELECT id FROM admin_users WHERE tenant_id = $1 AND email = $2`, tenant, w.email).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}
