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

	// ErrPasswordChangeSessionGone means the row could not be written while the change ran and the
	// request's session is no longer live (T179). Its usual cause is a link that ended it -- the link
	// by e-mail revokes the sessions of every row it links, each opened with the row's old password,
	// and the row's password became the identity's -- but it is decided by the session alone, so it
	// is also the answer when the row was disabled (its sessions revoked) and enabled again between
	// the write and the re-read. Nothing was changed. The request is answered as the next one would
	// be -- this browser is signed out.
	ErrPasswordChangeSessionGone = errors.New("adminauth: change password: the session ended while the change ran")

	// errRowLinkedMeanwhile is changeRowPassword's answer when SetOwnAdminPassword wrote nothing to
	// a row that is active when it is read again. Almost always the statement's other predicate,
	// `identity_id IS NULL`, is the one that failed -- a linker committed between the identity check
	// and the write (T179); a row inactive at the write and active again at the re-read looks the
	// same, and ChangeOwnPassword's identity re-ask and session check sort the two out.
	errRowLinkedMeanwhile = errors.New("adminauth: change password: the row was linked meanwhile")
)

// PasswordChange is what ChangeOwnPassword did.
type PasswordChange struct {
	// RevokedOtherSessions is how many OTHER live sessions of this business were revoked (K3); the
	// one that made the change survives.
	RevokedOtherSessions int
	// Recorded: the row is linked to an identity and identity_set_password changed the password of
	// the identity and every row linked to it, revoked the person's other sessions in EVERY business
	// and wrote each business its admin.password_changed row itself (00072) -- the caller writes no
	// row of its own (a second one would double this business's trail). False: today's one-row change,
	// and the caller writes the row as it always has.
	Recorded bool
}

// ChangeOwnPassword verifies the caller's CURRENT password and, if it matches and the NEW one
// differs, replaces the digest and revokes every OTHER live session -- the one that made the change
// survives (K3).
//
// 🔴 THROUGH THE IDENTITY WHEN THE ROW IS LINKED (ADR 0031 §5.1, §5.2, §8 -- M14-08's dual-write),
// WHATEVER TAPPA_ADMIN_IDENTITY SAYS. A linked row refuses a direct password write (00069's trigger:
// 23001), so today's one-row path would answer 500 to every linked person. session is the cookie's
// token -- the request's own, the one Verify resolved -- and its hash is the only key:
//
//	identity_password_for_session(hash)  -> the IDENTITY's digest; current and next are compared with
//	                                        it, the same two comparisons as below;
//	identity_set_password(hash, new)     -> the identity and every linked row take the new digest,
//	                                        every other session of the person in every business is
//	                                        revoked, each business gets its row.
//
// An identity_set_password that finds the session no longer live (a reset, a sign-out-everywhere or
// another change committed between the two calls revoked it) answers ErrNoSuchAdmin: nothing changed.
// A row that is NOT linked (pgx.ErrNoRows from the first call) takes today's path, unchanged: the
// tenant comes from the SESSION (§4.5), never the request, and SetOwnAdminPassword has no
// admin_user_id parameter that could name someone else.
//
// A ROW LINKED WHILE THE CHANGE RUNS (T179) -- between the identity check and today's UPDATE: a
// sign-in elsewhere linking it, the link by e-mail, the reconciliation. SetOwnAdminPassword writes
// unlinked rows only (`identity_id IS NULL`), so the race is 0 rows rather than 00069's trigger
// (restrict_violation, a 500); changeRowPassword rolls back and says so (errRowLinkedMeanwhile), and
// the change is asked of the identity ONCE more, exactly as if the request had come a moment later:
// the identity's digest (the same password after a reconciliation or a sign-in's link; the
// identity's own after the link by e-mail -- then the current password the person typed is
// rightly "not right"), then identity_set_password. No identity behind the session: a session
// no longer live (the link by e-mail ends it) is ErrPasswordChangeSessionGone, a live one
// ErrNoSuchAdmin; nothing changed either way.
func (m *Manager) ChangeOwnPassword(ctx context.Context, tenantID, adminID, exceptSessionID uuid.UUID, session Token, current, next string) (PasswordChange, error) {
	if tenantID == uuid.Nil || adminID == uuid.Nil {
		return PasswordChange{}, errors.New("adminauth: change password: tenant and admin are required")
	}
	h, err := session.hash(m.hmacKey)
	if err != nil {
		// The cookie Verify just accepted cannot be malformed; if it is, nothing is changed.
		return PasswordChange{}, ErrNoSuchAdmin
	}
	digest, err := m.data.IdentityPasswordForSession(ctx, h)
	switch {
	case err == nil:
		return m.changeIdentityPassword(ctx, h, digest.RevealForPasswordComparison(), current, next)
	case !errors.Is(err, pgx.ErrNoRows):
		return PasswordChange{}, fmt.Errorf("adminauth: change own credential: %w", err)
	}
	n, err := m.changeRowPassword(ctx, tenantID, adminID, exceptSessionID, current, next)
	if !errors.Is(err, errRowLinkedMeanwhile) {
		return PasswordChange{RevokedOtherSessions: n}, err
	}
	digest, err = m.data.IdentityPasswordForSession(ctx, h)
	switch {
	case err == nil:
		return m.changeIdentityPassword(ctx, h, digest.RevealForPasswordComparison(), current, next)
	case errors.Is(err, pgx.ErrNoRows):
		// No identity behind this session: either the session is no longer live (a link that
		// ended it, a disable in between) -- sign in again -- or the row was not linked after all
		// and its session lives (disabled at the write, enabled since): today's "no active admin".
		rs, serr := m.data.GetAdminSessionByTokenHash(ctx, h)
		switch {
		case errors.Is(serr, pgx.ErrNoRows) || (serr == nil && rs.RevokedAt != nil):
			return PasswordChange{}, ErrPasswordChangeSessionGone
		case serr != nil:
			return PasswordChange{}, fmt.Errorf("adminauth: change own credential: %w", serr)
		}
		return PasswordChange{}, ErrNoSuchAdmin
	default:
		return PasswordChange{}, fmt.Errorf("adminauth: change own credential: %w", err)
	}
}

// changeIdentityPassword is the linked row's change: the same two comparisons and the same Hash as
// the row's, against the identity's digest, then identity_set_password. Neither password nor digest
// leaves it (§4.7).
func (m *Manager) changeIdentityPassword(ctx context.Context, sessionHash, digest, current, next string) (PasswordChange, error) {
	if !Compare(digest, current) {
		return PasswordChange{}, ErrWrongCurrentPassword
	}
	if Compare(digest, next) {
		return PasswordChange{}, ErrSameAsCurrentPassword
	}
	newDigest, err := m.hash(next)
	if err != nil {
		return PasswordChange{}, fmt.Errorf("adminauth: change own credential: %w", err)
	}
	n, err := m.data.SetIdentityPassword(ctx, sessionHash, newDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordChange{}, ErrNoSuchAdmin
	}
	if err != nil {
		return PasswordChange{}, fmt.Errorf("adminauth: change own credential: %w", err)
	}
	return PasswordChange{RevokedOtherSessions: n, Recorded: true}, nil
}

// changeRowPassword is today's change of an UNLINKED row (M7-05, T73).
//
// EVERY STEP RUNS IN ONE WithTenant TRANSACTION (reset.go's pair-transaction shape): the read, the
// two comparisons, the hash, the write and the revocation share a fate, so a failure anywhere leaves
// the digest and the sessions exactly as they were.
func (m *Manager) changeRowPassword(ctx context.Context, tenantID, adminID, exceptSessionID uuid.UUID, current, next string) (int, error) {
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
		digest, e := m.hash(next)
		if e != nil {
			return e
		}

		// 5. Write it. 0 rows (pgx.ErrNoRows) means the account is not active -- the
		//    change is refused rather than silently no-op'd -- or the row was linked since the
		//    identity check (T179: the statement writes unlinked rows only). The row is read
		//    again to tell the two apart without naming identity_id (R16): under READ COMMITTED
		//    this read sees what the UPDATE saw, and an active row it could not write is linked.
		//    A pgx.ErrNoRows leaves the transaction usable; the rollback follows either way.
		_, e = q.SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{
			PasswordHash: digest, ID: adminID, TenantID: tenantID,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			again, re := q.GetAdminPasswordHashByID(ctx, store.GetAdminPasswordHashByIDParams{
				ID: adminID, TenantID: tenantID,
			})
			if re == nil && again.Status == "active" {
				return errRowLinkedMeanwhile
			}
			if re != nil && !errors.Is(re, pgx.ErrNoRows) {
				return re
			}
			return ErrNoSuchAdmin
		}
		if e != nil {
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
		errors.Is(err, ErrSameAsCurrentPassword),
		errors.Is(err, errRowLinkedMeanwhile):
		// The sentinels pass through unwrapped: they carry no value and the caller
		// branches on them by identity.
		return 0, err
	default:
		return 0, fmt.Errorf("adminauth: change own credential: %w", err)
	}
}
