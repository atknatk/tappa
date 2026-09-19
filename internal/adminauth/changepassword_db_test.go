package adminauth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// changepassword_db_test.go -- Manager.ChangeOwnPassword against a REAL Postgres (T73).
//
// It rides the same harness as manager_db_test.go and reset_db_test.go: testDB,
// testManager, newTenantRow, newAdminRow, newSessionRow, liveSessions and readDigest all
// live there. Fixtures are never cleaned up (tappa_app has REVOKE DELETE on the admin
// tables), so every row uses fresh random ids and a fresh random email.
//
// 🔴 NO REAL CREDENTIAL LIVES HERE. The passwords are literals used only to build and
// re-verify a digest for the length of one test, exactly as the sibling files do. The
// four cases below are §5-style: wrong current, new==current, the success path, and a
// disabled account -- each proving both the RETURN and that the stored digest moved (or
// did not) as it should.

// sessionRevoked reports whether one admin session has been revoked.
func sessionRevoked(t *testing.T, d *db.DB, tenantID, sessionID uuid.UUID) bool {
	t.Helper()
	var revoked bool
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE id = $1 AND tenant_id = $2`,
			sessionID, tenantID).Scan(&revoked)
	})
	if err != nil {
		t.Fatalf("read revoked_at: %v", err)
	}
	return revoked
}

// TestChangeOwnPassword_WrongCurrentIsRefusedAndHashUnchanged is §5's "prove you are
// still you": a wrong current password returns the sentinel and touches nothing.
func TestChangeOwnPassword_WrongCurrentIsRefusedAndHashUnchanged(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Wrong Current Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), "the-real-current-pw", "active", "owner", "Owner Person")
	before := readDigest(t, d, tenantID, admin)

	revoked, err := m.ChangeOwnPassword(ctx, tenantID, admin, uuid.New(), "not-the-current-pw", "a-fresh-new-password")
	if !errors.Is(err, ErrWrongCurrentPassword) {
		t.Fatalf("err = %v, want ErrWrongCurrentPassword", err)
	}
	if revoked != 0 {
		t.Errorf("revoked = %d, want 0 -- a refused change revokes nothing", revoked)
	}
	if after := readDigest(t, d, tenantID, admin); after != before {
		t.Errorf("the stored digest changed after a wrong-current refusal; it must not")
	}
}

// TestChangeOwnPassword_NewEqualToCurrentIsRefused is K-C: a new password equal to the
// current one is refused, so "I rotated my credential" is never a silent no-op.
func TestChangeOwnPassword_NewEqualToCurrentIsRefused(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	const pw = "same-password-both-ends"
	tenantID := newTenantRow(t, d, "Same Password Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), pw, "active", "owner", "Owner Person")
	before := readDigest(t, d, tenantID, admin)

	revoked, err := m.ChangeOwnPassword(ctx, tenantID, admin, uuid.New(), pw, pw)
	if !errors.Is(err, ErrSameAsCurrentPassword) {
		t.Fatalf("err = %v, want ErrSameAsCurrentPassword", err)
	}
	if revoked != 0 {
		t.Errorf("revoked = %d, want 0", revoked)
	}
	if after := readDigest(t, d, tenantID, admin); after != before {
		t.Errorf("the stored digest changed on a same-as-current refusal; it must not")
	}
}

// TestChangeOwnPassword_SucceedsAndRevokesOtherSessionsKeepingThisOne is the success
// path AND the K3 proof: the digest becomes the new one, every OTHER live session is
// revoked, and the session that made the change stays live.
func TestChangeOwnPassword_SucceedsAndRevokesOtherSessionsKeepingThisOne(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	const oldPW = "the-old-password-1234"
	const newPW = "a-genuinely-new-password-9999"
	tenantID := newTenantRow(t, d, "Rotate Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), oldPW, "active", "manager", "Manager Person")
	before := readDigest(t, d, tenantID, admin)

	// Three live sessions: this one (kept) and two others (revoked).
	keep := newSessionRow(t, d, tenantID, admin)
	other1 := newSessionRow(t, d, tenantID, admin)
	other2 := newSessionRow(t, d, tenantID, admin)
	if n := liveSessions(t, d, tenantID, admin); n != 3 {
		t.Fatalf("fixture: %d live sessions, want 3", n)
	}

	revoked, err := m.ChangeOwnPassword(ctx, tenantID, admin, keep, oldPW, newPW)
	if err != nil {
		t.Fatalf("ChangeOwnPassword: %v", err)
	}
	if revoked != 2 {
		t.Fatalf("revoked = %d, want 2 (the two OTHER sessions)", revoked)
	}

	// The digest moved to the new password.
	after := readDigest(t, d, tenantID, admin)
	if after == before {
		t.Errorf("the stored digest did not change on a successful rotation")
	}
	if !Compare(after, newPW) {
		t.Errorf("the stored digest does not verify the NEW password")
	}

	// K3: this session stays live; the other two are revoked; count agrees.
	if sessionRevoked(t, d, tenantID, keep) {
		t.Errorf("the session that made the change was revoked; it must survive (K3)")
	}
	if !sessionRevoked(t, d, tenantID, other1) || !sessionRevoked(t, d, tenantID, other2) {
		t.Errorf("an OTHER session survived the change; all but the current must be revoked")
	}
	if n := liveSessions(t, d, tenantID, admin); n != 1 {
		t.Errorf("%d live sessions after the change, want exactly 1 (this device)", n)
	}
}

// TestChangeOwnPassword_DisabledAdminIsRefusedAndHashUnchanged: a disabled account cannot
// rotate a credential. SetOwnAdminPassword's status guard yields 0 rows, the whole
// transaction rolls back, and the caller gets the "no active admin" sentinel.
func TestChangeOwnPassword_DisabledAdminIsRefusedAndHashUnchanged(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	const pw = "disabled-admins-password"
	tenantID := newTenantRow(t, d, "Disabled Admin Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), pw, "disabled", "manager", "Disabled Person")
	before := readDigest(t, d, tenantID, admin)

	revoked, err := m.ChangeOwnPassword(ctx, tenantID, admin, uuid.New(), pw, "a-new-password-for-nobody")
	if !errors.Is(err, ErrNoSuchAdmin) {
		t.Fatalf("err = %v, want ErrNoSuchAdmin", err)
	}
	if revoked != 0 {
		t.Errorf("revoked = %d, want 0", revoked)
	}
	if after := readDigest(t, d, tenantID, admin); after != before {
		t.Errorf("a disabled admin's digest changed; the status guard must have rolled it back")
	}
}
