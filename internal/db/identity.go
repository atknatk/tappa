package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The panel administrator's identity (ADR 0031, M14-04): the hand-written callers of the identity
// resolver and of the four session functions of the closed identity_* list (migration 00071).
//
// WHY HAND-WRITTEN: the resolve.go reason -- sqlc v1.28 cannot type a call to a `RETURNS TABLE(...)`
// function. Each const below names its columns, so a function whose result changed fails here, at
// the scan, not silently.
//
// WHY ON THE RAW POOL, WITH NO TENANT CONTEXT: like the md.7 resolvers, these calls PRODUCE a
// tenant (the session's, the switch's target, the login's landing) rather than run inside one. The
// functions are SECURITY DEFINER: resolve_identity_by_email is tappa_resolver's (md.7, read-only),
// the four others tappa_identitydefiner's -- ADR 0031 §5.2's closed list, CLAUDE.md §4.5's third
// exception. tappa_app holds EXECUTE on them and nothing on the identity tables.
//
// WHAT THEY TAKE: a session token's HASH (never the token), a new session's hash, a tenant id, and
// -- for OpenIdentitySession alone, ADR 0031's named exception 1 -- an identity id whose password
// the caller has verified. None of them is ever interpolated into an error (CLAUDE.md §7).
//
// 0 ROWS IS AN ANSWER, NOT A FAILURE: each function refuses by returning nothing, and each caller
// here turns that into pgx.ErrNoRows, so a caller tells "refused" from "the database failed" with
// errors.Is and gets one uniform refusal (ADR 0031 §5.4: no business / not a member / disabled are
// not told apart).

// ResolvedIdentity is resolve_identity_by_email's fixed column set: the identity an address names,
// its digest (compared in Go, never in SQL -- ADR 0031 §5.6), whether its mailbox was ever proved,
// and whether it holds at least one active, not-refused membership (R14: the login's fifth timing
// case is decided after the comparison, never before it).
//
// ID IS A CROSS-TENANT KEY. It travels to OpenIdentitySession and nowhere else: not into a log, a
// response, a template or an audit row.
type ResolvedIdentity struct {
	ID                  uuid.UUID
	PasswordHash        PasswordHash
	EmailVerifiedAt     *time.Time
	HasActiveMembership bool
}

// IdentitySession is a panel session an identity function opened: identity_open_session's or
// identity_switch_session's row. The token itself is the caller's; the database saw only its hash.
type IdentitySession struct {
	SessionID   uuid.UUID
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
}

// IdentityMembership is one row of identity_memberships: a business the session's person may switch
// to (or is in -- IsCurrent), its name, the person's role there and whether it is suspended (K-10:
// the switcher marks it, the switch is still allowed).
type IdentityMembership struct {
	TenantID   uuid.UUID
	TenantName string
	Role       string
	Suspended  bool
	IsCurrent  bool
}

// The SQL mirrors 00071's signatures; identitycatalog_test.go pins the signatures, identitysession_test.go
// sends these very consts as tappa_app and calls each method below on the application pool.
const resolveIdentityByEmailSQL = `SELECT id, password_hash, email_verified_at, has_active_membership
FROM resolve_identity_by_email($1::citext)`

const openIdentitySessionSQL = `SELECT session_id, tenant_id, admin_user_id
FROM identity_open_session($1, $2, $3, $4)`

const identityMembershipsSQL = `SELECT tenant_id, tenant_name, role, suspended, is_current
FROM identity_memberships($1)`

const switchIdentitySessionSQL = `SELECT session_id, tenant_id, admin_user_id
FROM identity_switch_session($1, $2, $3)`

const revokeIdentitySessionsSQL = `SELECT revoked
FROM identity_revoke_sessions($1, $2)`

// ResolveIdentityByEmail resolves an address to its identity, at most one (admin_identities.email is
// globally UNIQUE). pgx.ErrNoRows when no identity carries the address. It writes nothing.
func (d *DB) ResolveIdentityByEmail(ctx context.Context, email string) (ResolvedIdentity, error) {
	var (
		r      ResolvedIdentity
		digest string
	)
	err := d.pool.QueryRow(ctx, resolveIdentityByEmailSQL, email).Scan(&r.ID, &digest, &r.EmailVerifiedAt, &r.HasActiveMembership)
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("db: resolve identity by email: %w", err)
	}
	r.PasswordHash = NewPasswordHash(digest)
	return r, nil
}

// OpenIdentitySession opens a panel session for an identity WHOSE PASSWORD THE CALLER VERIFIED --
// ADR 0031 §5.2's named exception 1: the function is not keyed by a secret, so the caller's type is
// the guarantee (internal/adminauth's verified-identity type, M14-07/08; 00011 OBLIGATION 5's
// analogue). verified is the digest the password was compared with -- ResolvedIdentity.PasswordHash,
// handed back unchanged: the function opens nothing unless it is still the identity's digest under
// the person's lock (the user's decision of 2026-10-23 -- a login with the old password must not
// outlive a reset that committed while it waited). tenant is the business asked for, uuid.Nil for
// none: the session lands there if it is an active membership, else in the one used last, else in
// the oldest. pgx.ErrNoRows: no such identity, a stale digest, or no active membership.
func (d *DB) OpenIdentitySession(ctx context.Context, identityID, tenant uuid.UUID, sessionHash string, verified PasswordHash) (IdentitySession, error) {
	var want *uuid.UUID
	if tenant != uuid.Nil {
		want = &tenant
	}
	var s IdentitySession
	if err := d.pool.QueryRow(ctx, openIdentitySessionSQL, identityID, want, sessionHash, verified.RevealForPasswordComparison()).Scan(&s.SessionID, &s.TenantID, &s.AdminUserID); err != nil {
		return IdentitySession{}, fmt.Errorf("db: open identity session: %w", err)
	}
	return s, nil
}

// IdentityMemberships lists the businesses of the person behind a live session -- at most 50, the
// current one first; a session whose membership is not linked lists itself alone. Empty (not an
// error) when the session is not live: the caller's per-request gate has already refused such a
// session, and a list is never the authority for anything.
func (d *DB) IdentityMemberships(ctx context.Context, sessionHash string) ([]IdentityMembership, error) {
	rows, err := d.pool.Query(ctx, identityMembershipsSQL, sessionHash)
	if err != nil {
		return nil, fmt.Errorf("db: identity memberships: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (IdentityMembership, error) {
		var m IdentityMembership
		err := r.Scan(&m.TenantID, &m.TenantName, &m.Role, &m.Suspended, &m.IsCurrent)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("db: identity memberships: %w", err)
	}
	return out, nil
}

// SwitchIdentitySession moves a live, linked session to another business of the same person (ADR
// 0031 §5.4): a new session there under newSessionHash, the old one revoked, the two ordinary audit
// rows -- one transaction. pgx.ErrNoRows is the one uniform refusal: the session is not live or not
// linked, the target is the current business, or the person holds no active membership there. Of N
// concurrent switches of one session exactly one succeeds; the rest get pgx.ErrNoRows.
func (d *DB) SwitchIdentitySession(ctx context.Context, sessionHash string, target uuid.UUID, newSessionHash string) (IdentitySession, error) {
	var s IdentitySession
	if err := d.pool.QueryRow(ctx, switchIdentitySessionSQL, sessionHash, target, newSessionHash).Scan(&s.SessionID, &s.TenantID, &s.AdminUserID); err != nil {
		return IdentitySession{}, fmt.Errorf("db: switch identity session: %w", err)
	}
	return s, nil
}

// RevokeIdentitySessions signs the person behind a live, linked session out of every business,
// keeping the current session when keepCurrent; each revoked session leaves its own business an
// ordinary sign-out row. It returns how many were revoked. pgx.ErrNoRows when the session is not
// live or its membership is not linked -- the caller then revokes that one admin's sessions as today.
func (d *DB) RevokeIdentitySessions(ctx context.Context, sessionHash string, keepCurrent bool) (int, error) {
	var n int32
	if err := d.pool.QueryRow(ctx, revokeIdentitySessionsSQL, sessionHash, keepCurrent).Scan(&n); err != nil {
		return 0, fmt.Errorf("db: revoke identity sessions: %w", err)
	}
	return int(n), nil
}
