package adminauth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// identityops.go -- the panel session's operations THROUGH THE IDENTITY (ADR 0031 §5.2, §5.4; M14-08).
//
// THE FOUR OF THE CARD, AND WHERE EACH LIVES:
//
//	Open               AuthenticatePerson -> openIdentitySession (identity.go, M14-07): the ONE caller
//	                   of identity_open_session, reached only through verifiedIdentity (named
//	                   exception 1). Nothing new here.
//	Switch             below: identity_switch_session, keyed by the live session's token hash.
//	RevokeAll          below: identity_revoke_sessions, the same key; an unlinked row's session falls
//	                   back to today's one-admin revocation in its own business.
//	ChangeOwnPassword  changepassword.go: identity_password_for_session + identity_set_password for a
//	                   linked row, today's one-row path otherwise.
//
// 🔴 THE KEY IS THE COOKIE'S TOKEN, HASHED HERE, AND NOTHING ELSE. Every function below finds the
// person through admin_sessions.token_hash (ADR 0031 §5.2: an identity writer finds the identity
// only through a secret). A tenant or an admin id from the request is never an input to an identity
// function -- the switch's target is a CHOICE the identity's own memberships either contain or not.
//
// 🔴 VERIFY IS UNTOUCHED. The per-request path (Verify -> TouchAdminSession) reads no identity and
// does not change with M14-08 (ADR 0031 §5.3); a switched session is an ordinary session of its new
// business to it.
//
// THE FLAG DOES NOT GATE THESE. They are the mechanics; whether a request may REACH them is the
// handler's gate (IdentityGate, flag.go) -- /admin/switch and "sign out everywhere" are closed while
// TAPPA_ADMIN_IDENTITY is off, the password change is a write path and is never closed.

// ErrSwitchRefused is the switch's ONE refusal (ADR 0031 §5.4): the session is not live or its row is
// not linked, the target is the current business, the person holds no active membership there, or a
// concurrent switch of the same session won. They are deliberately not told apart -- "no such
// business", "not a member" and "disabled" must read the same.
var ErrSwitchRefused = errors.New("adminauth: this business cannot be switched to")

// Switch moves the session the cookie carries to target, another business of the same person (ADR
// 0031 §5.4): a NEW session there under a new token, the old one revoked, authenticated_at copied
// from the old session's row, the two ordinary audit rows -- one transaction in the database. The
// old token resolves to nothing afterwards; the caller sets the new one. Of N concurrent switches of
// one session exactly one succeeds; the rest get ErrSwitchRefused and must not touch the cookie.
func (m *Manager) Switch(ctx context.Context, current Token, target uuid.UUID) (Issued, error) {
	h, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return Issued{}, ErrNoSession
		}
		return Issued{}, fmt.Errorf("adminauth: switch: %w", err)
	}
	if target == uuid.Nil {
		return Issued{}, ErrSwitchRefused
	}
	t, err := newToken()
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: switch: %w", err)
	}
	nh, err := t.hash(m.hmacKey)
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: switch: %w", err)
	}
	s, err := m.data.SwitchIdentitySession(ctx, h, target, nh)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issued{}, ErrSwitchRefused
	}
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: switch: %w", err)
	}
	return Issued{
		Session: Session{ID: s.SessionID, TenantID: s.TenantID, AdminUserID: s.AdminUserID},
		Token:   t,
	}, nil
}

// ErrRevokeAllSessionGone is RevokeAll's refusal on the one-business fallback (keepCurrent false):
// the request's own session -- verified moments earlier -- was not among the sessions the fallback
// revoked, i.e. it died in between (a reset, another sign-out, a concurrent change of the row). The
// identity's revocation answers 0 rows for exactly that case too, so the fallback cannot tell "this
// row is not linked" from "this session is no longer a key" -- and revoking this business alone
// while the person believes every business was signed out is the wrong answer to the second. So
// nothing is revoked (the fallback's transaction is rolled back) and the person is asked to sign in
// again and press once more (M14-11 security review).
var ErrRevokeAllSessionGone = errors.New("adminauth: revoke all: the session is no longer live")

// Revocation is what RevokeAll did.
type Revocation struct {
	// Revoked is how many sessions were revoked -- in every business when ThroughIdentity, in this
	// business only otherwise. A number for the person's own response and a log line, never for an
	// audit row (it would tell one business how many the person holds elsewhere).
	Revoked int
	// ThroughIdentity: the row is linked, identity_revoke_sessions revoked the person's sessions in
	// every business and wrote each its ordinary sign-out rows itself -- the caller writes none.
	ThroughIdentity bool
	// Sessions are the revoked sessions of THIS business when !ThroughIdentity (the row is not linked):
	// the caller writes their sign-out rows, as Logout does for one.
	Sessions []uuid.UUID
}

// RevokeAll signs the person behind the cookie out everywhere (ADR 0031 §5.2), keeping the current
// session when keepCurrent. A linked row's session goes through the identity: every live session of
// every membership of the person, in every business. An unlinked row's session is today's case: the
// admin's own sessions in this business (tenantID, adminID and sessionID are the request's VERIFIED
// session -- httpx.AdminOf -- and are used for that fallback alone). With keepCurrent false the
// fallback must revoke the request's own session among the others; when it does not, nothing is
// revoked and the answer is ErrRevokeAllSessionGone.
func (m *Manager) RevokeAll(ctx context.Context, tenantID, adminID, sessionID uuid.UUID, current Token, keepCurrent bool) (Revocation, error) {
	if tenantID == uuid.Nil || adminID == uuid.Nil || sessionID == uuid.Nil {
		return Revocation{}, errors.New("adminauth: revoke all: tenant, admin and session are required")
	}
	h, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return Revocation{}, ErrNoSession
		}
		return Revocation{}, fmt.Errorf("adminauth: revoke all: %w", err)
	}
	n, err := m.data.RevokeIdentitySessions(ctx, h, keepCurrent)
	if err == nil {
		return Revocation{Revoked: n, ThroughIdentity: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Revocation{}, fmt.Errorf("adminauth: revoke all: %w", err)
	}
	// Not linked (or not live): this admin, this business, as before M14.
	var ids []uuid.UUID
	var gone bool
	err = m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var e error
		if keepCurrent {
			ids, e = q.RevokeOtherAdminSessionsForAdmin(ctx, store.RevokeOtherAdminSessionsForAdminParams{
				TenantID: tenantID, AdminUserID: adminID, ExceptSessionID: sessionID,
			})
			return e
		}
		ids, e = q.RevokeAdminSessionsForAdmin(ctx, store.RevokeAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID,
		})
		if e == nil && !slices.Contains(ids, sessionID) {
			// The verified session was already dead: roll the revocation back (see
			// ErrRevokeAllSessionGone).
			gone = true
			return ErrRevokeAllSessionGone
		}
		return e
	})
	if gone {
		return Revocation{}, ErrRevokeAllSessionGone
	}
	if err != nil {
		return Revocation{}, fmt.Errorf("adminauth: revoke all: %w", err)
	}
	return Revocation{Revoked: len(ids), Sessions: ids}, nil
}
