package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// adminchangepassword_test.go -- the T73 (M7-05) data-layer proofs for
// "an administrator changes their OWN password from the panel", against a REAL
// Postgres (CLAUDE.md section 8 / Q04). T73 adds NO migration: it adds three sqlc
// queries over tables that already exist, so every claim below is measured with a
// real statement rather than asserted.
//
// The three queries and what each is proven to do:
//   - GetAdminPasswordHashByID -- reads the caller's OWN digest so adminauth can
//     bcrypt.Compare the current password. It is the SECOND (and last) reader of
//     password_hash in the product; the cross-tenant proof here is that a hash is the
//     one value another tenant must never reach.
//   - SetOwnAdminPassword -- writes the new digest, scoped to (id, tenant, active).
//     0 rows -> pgx.ErrNoRows -> caller rejects.
//   - RevokeOtherAdminSessionsForAdmin -- K3: signs the admin out everywhere EXCEPT
//     the session that made the change.
//
// No test here holds a real password. The starting digest is
// fixtures.UnusablePasswordHash (a hand-written bcrypt SHAPE that hashes nothing) and
// the "new" digest is fakeNewPasswordHash below -- both are bcrypt-shaped so migration
// 00018's CHECK accepts them, and neither is a pre-image of any password (section 4.7).
// Fixtures are NOT cleaned up (store_test.go's reason: tappa_app has REVOKE DELETE on
// both admin tables, so the impossibility of teardown IS the audit guarantee).

// fakeNewPasswordHash is the value SetOwnAdminPassword writes in these tests. It is
// bcrypt's SHAPE -- `$2a$12$` followed by 53 characters of the bcrypt base64 alphabet
// -- so migration 00018's CHECK (^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$)
// accepts it, and it is DELIBERATELY DIFFERENT from fixtures.UnusablePasswordHash
// ('$2a$04$' + 53 'c') so a "the digest actually changed" assertion is not vacuous. It
// is not a hash of anything and is never bcrypt-verified here; the test only checks the
// stored string equals what it wrote (section 4.7: fakes, not secrets).
const fakeNewPasswordHash = "$2a$12$" +
	"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" // 53

// getOwnHash reads an admin's stored digest through the real query, in that admin's
// OWN tenant context. Used to establish and re-check the "unchanged" fact.
func getOwnHash(t *testing.T, d *DB, tenantID, adminID uuid.UUID) store.GetAdminPasswordHashByIDRow {
	t.Helper()
	var row store.GetAdminPasswordHashByIDRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).GetAdminPasswordHashByID(ctx, store.GetAdminPasswordHashByIDParams{
			ID: adminID, TenantID: tenantID,
		})
		return e
	}); err != nil {
		t.Fatalf("GetAdminPasswordHashByID(own): %v", err)
	}
	return row
}

// sessionRevoked reports whether a given session id currently has revoked_at set,
// read through ListAdminSessionsForAdmin in the admin's own context (the history
// stays visible, so a revoked row is still listed -- section 4.6).
func sessionRevoked(t *testing.T, d *DB, tenantID, adminID, sessionID uuid.UUID) bool {
	t.Helper()
	var list []store.ListAdminSessionsForAdminRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		list, e = store.New(tx).ListAdminSessionsForAdmin(ctx, store.ListAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID,
		})
		return e
	}); err != nil {
		t.Fatalf("ListAdminSessionsForAdmin: %v", err)
	}
	for _, r := range list {
		if r.ID == sessionID {
			return r.RevokedAt != nil
		}
	}
	t.Fatalf("session %s vanished from the list; the trail must stay visible (section 4.6)", sessionID)
	return false
}

// ------------------------------------------------------------- isolation ----

// TestRLS_AdminChangePassword_TenantIsolation is the mandatory cross-tenant proof
// (CLAUDE.md section 4.5 / section 8) over the surface T73 adds: tenant A must not be
// able to READ, REWRITE or REVOKE anything belonging to tenant B through any of the
// three new queries.
//
// It proves isolation TWICE, because the two shapes prove different things
// (CLAUDE.md section 6):
//   - through the sqlc queries, which carry an EXPLICIT tenant_id predicate: this is
//     the production shape and proves the belt is correct (0 rows -> ErrNoRows).
//   - through RAW statements carrying NO tenant predicate: here a 0 can ONLY come from
//     RLS, so these are the true RLS proof and would go red with RLS switched off.
//     Each is paired with a positive control in B's own context so it cannot pass
//     vacuously.
func TestRLS_AdminChangePassword_TenantIsolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app) // runtime proof: tappa_app, no super / no bypass RLS

	tenantA, _ := newTenant(t, app)
	tenantB, _ := newTenant(t, app)
	adminB := newAdmin(t, app, tenantB, randAdminEmail(t), "active", "owner")
	sessionB1, _ := newAdminSession(t, app, tenantB, adminB)
	sessionB2, _ := newAdminSession(t, app, tenantB, adminB)

	// --- READ: A cannot read B's digest ---------------------------------------

	// (1) The production query, with its explicit predicate: cross-tenant -> ErrNoRows.
	err := app.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).GetAdminPasswordHashByID(ctx, store.GetAdminPasswordHashByIDParams{
			ID: adminB, TenantID: tenantA,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetAdminPasswordHashByID(B's id, A's tenant): err = %v, want pgx.ErrNoRows", err)
	}

	// (2) The RLS proof: a raw count with NO tenant predicate. A hash is the one value
	// a cross-tenant read must never reach, so this probes the row that carries it.
	if self := rawCountCtx(t, app, tenantB,
		`SELECT count(*) FROM admin_users WHERE id = $1`, adminB); self != 1 {
		t.Fatalf("positive control: B's context sees %d of its own admin rows, want 1 (isolation check would be vacuous)", self)
	}
	if cross := rawCountCtx(t, app, tenantA,
		`SELECT count(*) FROM admin_users WHERE id = $1`, adminB); cross != 0 {
		t.Fatalf("RLS FAILED: A's context read %d of B's admin rows, want 0", cross)
	}

	// --- WRITE: A cannot rewrite B's digest -----------------------------------

	before := getOwnHash(t, app, tenantB, adminB)
	if before.PasswordHash != fixtures.UnusablePasswordHash {
		t.Fatalf("fixture digest = %q, want the placeholder; the test cannot trust its baseline", before.PasswordHash)
	}

	// (3) The production query cross-tenant: 0 rows -> ErrNoRows, nothing written.
	err = app.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{
			ID: adminB, TenantID: tenantA, PasswordHash: fakeNewPasswordHash,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetOwnAdminPassword(B's id, A's tenant): err = %v, want pgx.ErrNoRows", err)
	}

	// (4) The RLS proof: a raw UPDATE with NO tenant predicate. Under A's context
	// RLS USING hides B's row, so 0 rows are affected -- the 0 cannot be blamed on a
	// WHERE that is not there.
	var affected int64
	if err := app.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx,
			`UPDATE admin_users SET password_hash = $2 WHERE id = $1`, adminB, fakeNewPasswordHash)
		if e != nil {
			return e
		}
		affected = tag.RowsAffected()
		return nil
	}); err != nil {
		t.Fatalf("raw cross-tenant UPDATE admin_users: %v", err)
	}
	if affected != 0 {
		t.Fatalf("RLS FAILED: A's context rewrote %d of B's admin rows, want 0", affected)
	}

	// B's digest is byte-for-byte what it was: neither path touched it.
	if after := getOwnHash(t, app, tenantB, adminB); after.PasswordHash != before.PasswordHash {
		t.Fatalf("B's password_hash changed under A's attempts: before=%q after=%q", before.PasswordHash, after.PasswordHash)
	}

	// --- REVOKE: A cannot revoke B's sessions ---------------------------------

	// (5) The production query cross-tenant: returns no ids and revokes nothing.
	var revoked []uuid.UUID
	if err := app.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		revoked, e = store.New(tx).RevokeOtherAdminSessionsForAdmin(ctx, store.RevokeOtherAdminSessionsForAdminParams{
			TenantID: tenantA, AdminUserID: adminB, ExceptSessionID: sessionB1,
		})
		return e
	}); err != nil {
		t.Fatalf("RevokeOtherAdminSessionsForAdmin(A ctx, B admin): %v", err)
	}
	if len(revoked) != 0 {
		t.Fatalf("A's context revoked %d of B's sessions through the query, want 0", len(revoked))
	}

	// (6) The RLS proof: a raw revoke-all with NO tenant predicate. 0 rows can only be RLS.
	if err := app.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx,
			`UPDATE admin_sessions SET revoked_at = now() WHERE admin_user_id = $1 AND revoked_at IS NULL`, adminB)
		if e != nil {
			return e
		}
		affected = tag.RowsAffected()
		return nil
	}); err != nil {
		t.Fatalf("raw cross-tenant UPDATE admin_sessions: %v", err)
	}
	if affected != 0 {
		t.Fatalf("RLS FAILED: A's context revoked %d of B's sessions raw, want 0", affected)
	}

	// Both of B's sessions are still live: nothing A did reached them.
	if sessionRevoked(t, app, tenantB, adminB, sessionB1) {
		t.Fatal("B's session 1 was revoked by A's cross-tenant attempts")
	}
	if sessionRevoked(t, app, tenantB, adminB, sessionB2) {
		t.Fatal("B's session 2 was revoked by A's cross-tenant attempts")
	}
}

// ---------------------------------------------------------- happy path -------

// TestAdminChangePassword_OwnTenant is the positive control for all three queries in
// the caller's OWN tenant: the read returns the digest, the write replaces it, and
// the revoke signs out every OTHER session while KEEPING the current one -- the whole
// point of K3.
func TestAdminChangePassword_OwnTenant(t *testing.T) {
	app := appDB(t)
	tenantID, _ := newTenant(t, app)
	adminID := newAdmin(t, app, tenantID, randAdminEmail(t), "active", "owner")

	// READ: the current digest and status come back.
	got := getOwnHash(t, app, tenantID, adminID)
	if got.ID != adminID {
		t.Fatalf("GetAdminPasswordHashByID returned id %s, want %s", got.ID, adminID)
	}
	if got.PasswordHash != fixtures.UnusablePasswordHash {
		t.Fatalf("GetAdminPasswordHashByID returned an unexpected digest %q", got.PasswordHash)
	}
	if got.Status != "active" {
		t.Fatalf("GetAdminPasswordHashByID returned status %q, want active", got.Status)
	}

	// WRITE: SetOwnAdminPassword replaces the digest and returns the caller's id.
	var setID uuid.UUID
	if err := app.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		setID, e = store.New(tx).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{
			ID: adminID, TenantID: tenantID, PasswordHash: fakeNewPasswordHash,
		})
		return e
	}); err != nil {
		t.Fatalf("SetOwnAdminPassword(own): %v", err)
	}
	if setID != adminID {
		t.Fatalf("SetOwnAdminPassword returned id %s, want %s", setID, adminID)
	}
	if after := getOwnHash(t, app, tenantID, adminID); after.PasswordHash != fakeNewPasswordHash {
		t.Fatalf("digest after set = %q, want the new value (the UPDATE(password_hash) grant is missing?)", after.PasswordHash)
	}

	// REVOKE-OTHERS: three live sessions; revoking others keeps exactly the current one.
	sCurrent, _ := newAdminSession(t, app, tenantID, adminID)
	sOther1, _ := newAdminSession(t, app, tenantID, adminID)
	sOther2, _ := newAdminSession(t, app, tenantID, adminID)

	var revoked []uuid.UUID
	if err := app.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		revoked, e = store.New(tx).RevokeOtherAdminSessionsForAdmin(ctx, store.RevokeOtherAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID, ExceptSessionID: sCurrent,
		})
		return e
	}); err != nil {
		t.Fatalf("RevokeOtherAdminSessionsForAdmin(own): %v", err)
	}
	gotRevoked := map[uuid.UUID]bool{}
	for _, id := range revoked {
		gotRevoked[id] = true
	}
	if len(revoked) != 2 || !gotRevoked[sOther1] || !gotRevoked[sOther2] {
		t.Fatalf("revoked ids = %v, want exactly {%s, %s}", revoked, sOther1, sOther2)
	}
	if gotRevoked[sCurrent] {
		t.Fatal("RevokeOtherAdminSessionsForAdmin revoked the CURRENT session; K3 requires it survive")
	}
	// The catalog agrees: current live, others dead.
	if sessionRevoked(t, app, tenantID, adminID, sCurrent) {
		t.Fatal("the current session is revoked; the user was signed out of the device that changed the password")
	}
	if !sessionRevoked(t, app, tenantID, adminID, sOther1) || !sessionRevoked(t, app, tenantID, adminID, sOther2) {
		t.Fatal("an OTHER session is still live after revoke-others")
	}

	// IDEMPOTENT: with the others already dead, a second call revokes nothing and
	// still spares the current session.
	if err := app.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		again, e := store.New(tx).RevokeOtherAdminSessionsForAdmin(ctx, store.RevokeOtherAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID, ExceptSessionID: sCurrent,
		})
		if e != nil {
			return e
		}
		if len(again) != 0 {
			t.Fatalf("second revoke-others returned %d ids, want 0 (idempotent)", len(again))
		}
		return nil
	}); err != nil {
		t.Fatalf("second RevokeOtherAdminSessionsForAdmin: %v", err)
	}
	if sessionRevoked(t, app, tenantID, adminID, sCurrent) {
		t.Fatal("the current session was revoked by the idempotent second call")
	}
}

// TestSetOwnAdminPassword_RefusesDisabledAdmin proves the status = 'active' guard: a
// disabled admin has no business rotating a credential, so the write collapses to
// pgx.ErrNoRows exactly like an unknown id -- the caller rejects rather than no-op's.
func TestSetOwnAdminPassword_RefusesDisabledAdmin(t *testing.T) {
	app := appDB(t)
	tenantID, _ := newTenant(t, app)
	disabledID := newAdmin(t, app, tenantID, randAdminEmail(t), "disabled", "manager")

	err := app.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{
			ID: disabledID, TenantID: tenantID, PasswordHash: fakeNewPasswordHash,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetOwnAdminPassword(disabled admin): err = %v, want pgx.ErrNoRows", err)
	}
	// The digest is untouched: the placeholder is still there.
	if got := getOwnHash(t, app, tenantID, disabledID); got.PasswordHash != fixtures.UnusablePasswordHash {
		t.Fatalf("a disabled admin's digest changed to %q", got.PasswordHash)
	}
}

// TestSetOwnAdminPassword_RejectsNonBcryptDigest proves migration 00018's CHECK backs
// the "password_hash is only ever written with adminauth.Hash output" rule at the
// database: a value bcrypt cannot process is refused with 23514, the whole
// transaction rolls back, and the digest is unchanged -- fail-closed. This is the
// belt under the discipline, not the discipline itself.
func TestSetOwnAdminPassword_RejectsNonBcryptDigest(t *testing.T) {
	app := appDB(t)
	tenantID, _ := newTenant(t, app)
	adminID := newAdmin(t, app, tenantID, randAdminEmail(t), "active", "owner")

	err := app.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{
			ID: adminID, TenantID: tenantID, PasswordHash: "not-a-bcrypt-hash",
		})
		return e
	})
	if err == nil {
		t.Fatal("SetOwnAdminPassword accepted a non-bcrypt digest; 00018's CHECK is not in force")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetOwnAdminPassword returned ErrNoRows for a bad digest, want a 23514 CHECK violation")
	}
	pg := asPgErr(err)
	if pg == nil || pg.Code != "23514" {
		t.Fatalf("want a 23514 check_violation, got %v", err)
	}
	if !strings.Contains(pg.ConstraintName, "password_hash") {
		t.Fatalf("23514 fired on constraint %q, want the password_hash bcrypt check", pg.ConstraintName)
	}
	// Unchanged: the placeholder survives the rejected write.
	if got := getOwnHash(t, app, tenantID, adminID); got.PasswordHash != fixtures.UnusablePasswordHash {
		t.Fatalf("digest changed to %q despite the CHECK rejection", got.PasswordHash)
	}
}
