package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

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
	// THE NEW SESSION ENDS WHEN THE OLD ONE WOULD HAVE (T183, ADR 0035): the switch copied the
	// password proof, and a session lives db.AdminSessionLifetime from its proof, so the cookie gets
	// the remaining time -- read in the new business's context, by the database's clock. 0 rows means
	// it ended between the switch and this read (its last second): the switch is refused like any
	// other, the response writes no cookie, and the browser's revoked one sends it to the sign-in page.
	var left int32
	err = m.data.WithTenant(ctx, s.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		left, e = store.New(tx).GetAdminSessionSecondsLeft(ctx, store.GetAdminSessionSecondsLeftParams{ID: s.SessionID, TenantID: s.TenantID})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && left <= 0) {
		return Issued{}, ErrSwitchRefused
	}
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: switch: %w", err)
	}
	return Issued{
		Session: Session{ID: s.SessionID, TenantID: s.TenantID, AdminUserID: s.AdminUserID},
		Token:   t,
		Left:    time.Duration(left) * time.Second,
	}, nil
}

// switchBucketLabel is the domain of the switch's rate bucket under this package's key.
const switchBucketLabel = "tappa/admin-switch-bucket/v1|"

// switchBucketOf is Resolved.switchBucket for a row whose identity_id is identity (nil: an
// unlinked row, the zero bucket). A MAC rather than the id, so the bucket can key a limiter
// without the id ever leaving this package.
func (m *Manager) switchBucketOf(identity *uuid.UUID) [sha256.Size]byte {
	var out [sha256.Size]byte
	if identity == nil || *identity == uuid.Nil {
		return out
	}
	h := hmac.New(sha256.New, m.hmacKey)
	_, _ = h.Write([]byte(switchBucketLabel + identity.String())) // hash writes never fail
	copy(out[:], h.Sum(nil))
	return out
}

// SwitchBucket is the switch's rate bucket of the person behind this session (ADR 0031 §5.4):
// the same value for every session of every membership of one identity, and nothing that names
// the identity. ok is false for an unlinked row, which has no person to key by -- its switch is
// refused anyway (identity_switch_session answers 0 rows), and the caller keys it by the session.
func (r Resolved) SwitchBucket() (bucket string, ok bool) {
	if r.switchBucket == ([sha256.Size]byte{}) {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(r.switchBucket[:]), true
}

// Membership is one business of the person behind a session, for the switch page (ADR 0031
// §5.4): display data and the target a switch form names. It is never the authority for a
// switch -- identity_switch_session re-proves the membership under the person's lock.
type Membership struct {
	TenantID   uuid.UUID
	TenantName string
	Role       string
	Suspended  bool
	Current    bool
}

// Memberships lists the businesses of the person behind the cookie's session -- at most 50, the
// current one first (identity_memberships, 00071). An unlinked row lists its own business alone;
// a session that is not live lists nothing. Keyed by the cookie's token, like Switch: a tenant
// from the request is never an input.
func (m *Manager) Memberships(ctx context.Context, current Token) ([]Membership, error) {
	h, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return nil, ErrNoSession
		}
		return nil, fmt.Errorf("adminauth: memberships: %w", err)
	}
	rows, err := m.data.IdentityMemberships(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("adminauth: memberships: %w", err)
	}
	out := make([]Membership, 0, len(rows))
	for _, r := range rows {
		out = append(out, Membership{TenantID: r.TenantID, TenantName: r.TenantName, Role: r.Role, Suspended: r.Suspended, Current: r.IsCurrent})
	}
	return out, nil
}

// ErrRevokeAllSessionGone is RevokeAll's refusal on the one-business fallback (keepCurrent false):
// the request's own session -- verified moments earlier -- was not among the sessions the fallback
// revoked, i.e. it died in between (a reset, another sign-out, a concurrent change of the row). The
// identity's revocation answers 0 rows for exactly that case too, so the fallback cannot tell "this
// row is not linked" from "this session is no longer a key" -- and revoking this business alone
// while the person believes every business was signed out is the wrong answer to the second. So
// nothing is revoked (the fallback's transaction is rolled back) and the person is asked to sign in
// again and press once more (M14-11 security review).
//
// A LINKED session never takes the fallback at all: when the caller says the verified row was linked
// and the identity's revocation answers 0 rows, the session or its membership died in between (a
// reset, a concurrent disabling of the membership -- which the fallback's statement does not look
// at); the fallback would sign out this business alone. That is this error too (M14-11 re-audit).
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
// session -- httpx.AdminOf -- and are used for that fallback alone). linked is the verified session's
// Resolved.Linked: a linked session is never given the fallback (ErrRevokeAllSessionGone instead).
// With keepCurrent false the
// fallback must revoke the request's own session among the others; when it does not, nothing is
// revoked and the answer is ErrRevokeAllSessionGone.
func (m *Manager) RevokeAll(ctx context.Context, tenantID, adminID, sessionID uuid.UUID, linked bool, current Token, keepCurrent bool) (Revocation, error) {
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
	if linked {
		// The row was linked when the request was verified, so 0 rows here is a session or a
		// membership that died since -- never "today's unlinked row" (ErrRevokeAllSessionGone).
		return Revocation{}, ErrRevokeAllSessionGone
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
