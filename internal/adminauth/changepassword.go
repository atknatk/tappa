package adminauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// changepassword.go -- CHANGE MY OWN PASSWORD FROM THE PANEL (M7-05, T73).
//
// ITS AUTHORITY IS THE CURRENT PASSWORD, NOT A MAILED TOKEN, which is the line
// db/queries/admins.sql draws between this flow and the reset flow: reset proves
// possession of a link, this proves possession of the credential, and the two never
// share a statement. An authenticated admin -- owner OR manager, because a password is
// personal (K-A) -- re-types their CURRENT password and chooses a new one; "prove you
// are still you" is what defends against a walked-away laptop.
//
// 🔴 §4.7 -- NEITHER PASSWORD NOR DIGEST EVER LEAVES THIS FUNCTION. current, next and
// the digest are compared or written and never logged, never wrapped into an error and
// never returned. The sentinels below carry NO value: a caller that branches on them
// learns only WHICH policy failed, never the input that failed it. The only fmt.Errorf
// here wraps a pgx/database error, whose text never contains a credential.

var (
	// ErrWrongCurrentPassword means the CURRENT password the caller typed does not
	// match the stored digest. It is its own sentinel so the handler can show the one
	// message this deserves ("your current password is not right") without the manager
	// telling wrong-current apart from same-as-current at the timing level -- both run
	// a full bcrypt.Compare (K-B).
	ErrWrongCurrentPassword = errors.New("adminauth: current password does not match")
	// ErrSameAsCurrentPassword means the NEW password equals the current one. K-C: a
	// change that changes nothing is refused rather than written, so "I rotated my
	// credential" is never a no-op the audit row would nonetheless assert happened.
	ErrSameAsCurrentPassword = errors.New("adminauth: new password equals the current one")
	// The "no active admin for that id" case reuses reset.go's ErrNoSuchAdmin: an
	// unknown/wrong-tenant id (the read finds nothing) and a disabled account (the write
	// updates nothing) are the same outcome to the caller -- there is no active admin to
	// rotate a credential for.
)

// ChangeOwnPassword verifies the caller's CURRENT password and, if it matches and the
// NEW one differs, replaces the digest and revokes every OTHER live session of the same
// admin -- the one that made the change survives (K3). It returns how many other
// sessions were revoked.
//
// EVERY STEP RUNS IN ONE WithTenant TRANSACTION (reset.go's pair-transaction shape): the
// read, the two comparisons, the hash, the write and the revocation share a fate, so a
// failure anywhere leaves the digest and the sessions exactly as they were. The tenant
// comes from the SESSION (§4.5), never the request; the id can only ever be the caller's
// own, because SetOwnAdminPassword has no admin_user_id parameter that could name
// someone else.
func (m *Manager) ChangeOwnPassword(ctx context.Context, tenantID, adminID, exceptSessionID uuid.UUID, current, next string) (int, error) {
	if tenantID == uuid.Nil || adminID == uuid.Nil {
		return 0, errors.New("adminauth: change password: tenant and admin are required")
	}

	var revoked int
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)

		// 1. Read the caller's OWN digest (id from the live session, tenant from the
		//    request context). status is returned, not filtered here -- the write below
		//    is where a disabled account is refused.
		row, e := q.GetAdminPasswordHashByID(ctx, store.GetAdminPasswordHashByIDParams{
			ID: adminID, TenantID: tenantID,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNoSuchAdmin
		}
		if e != nil {
			return e
		}

		// 2. K-B: prove the current password. Compare is constant-time (bcrypt pays the
		//    whole key schedule before it compares -- password.go), so this reveals
		//    nothing by timing.
		if !Compare(row.PasswordHash, current) {
			return ErrWrongCurrentPassword
		}

		// 3. K-C: the new password must differ from the current one. Compared against the
		//    same stored digest so "new == current" is a real match, not a string check
		//    that a re-typed-but-equivalent value could slip past.
		if Compare(row.PasswordHash, next) {
			return ErrSameAsCurrentPassword
		}

		// 4. Hash the new password. adminauth.Hash is the ONLY producer of a value written
		//    to password_hash; it refuses an empty or over-72-byte input, and migration
		//    00018's CHECK refuses at the database anything Hash would not have produced.
		digest, e := Hash(next)
		if e != nil {
			return e
		}

		// 5. Write it. 0 rows (pgx.ErrNoRows) means the account is not active -- the
		//    change is refused rather than silently no-op'd.
		if _, e = q.SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{
			PasswordHash: digest, ID: adminID, TenantID: tenantID,
		}); e != nil {
			return e
		}

		// 6. K3: revoke every OTHER live session, keeping the one that made the change.
		ids, e := q.RevokeOtherAdminSessionsForAdmin(ctx, store.RevokeOtherAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID, ExceptSessionID: exceptSessionID,
		})
		if e != nil {
			return e
		}
		revoked = len(ids)
		return nil
	})

	switch {
	case err == nil:
		return revoked, nil
	case errors.Is(err, ErrNoSuchAdmin),
		errors.Is(err, ErrWrongCurrentPassword),
		errors.Is(err, ErrSameAsCurrentPassword):
		// The sentinels pass through unwrapped: they carry no value and the caller
		// branches on them by identity.
		return 0, err
	case errors.Is(err, pgx.ErrNoRows):
		// Only SetOwnAdminPassword can reach here (step 1's ErrNoRows was already mapped):
		// the row exists but status != 'active'. Collapse it to the same "no active admin"
		// outcome as an unknown id -- a disabled admin has no business rotating a credential.
		return 0, ErrNoSuchAdmin
	default:
		return 0, fmt.Errorf("adminauth: change own credential: %w", err)
	}
}
