package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The account page's identity calls (ADR 0031 §5.2, §5.7, §5.8; M14-18): the e-mail verification
// (00072), the confirmation window -- its read and "Keep all" (00079) -- and the window's two acts,
// unlink and its Undo (00072). identity.go's rules hold: hand-written because sqlc cannot type a
// RETURNS TABLE call; on the raw pool, no tenant context (each call is keyed by a session token's
// HASH and produces what it needs); 0 rows is the refusal, turned into pgx.ErrNoRows; nothing here
// ever puts an argument into an error (CLAUDE.md §7).

const requestIdentityVerificationSQL = `SELECT verification_id, expires_at
FROM identity_request_verification($1, $2)`

const verifyIdentityEmailSQL = `SELECT first_verification
FROM identity_verify_email($1, $2)`

const confirmWindowSQL = `SELECT verified, scope, open_until, tenant_id, tenant_name, role, refused, last_active_owner
FROM identity_confirm_window($1)`

const keepAllSQL = `SELECT kept
FROM identity_keep_all($1)`

const unlinkMembershipSQL = `SELECT admin_user_id, revoked_sessions
FROM identity_unlink_backfilled($1, $2)`

const relinkMembershipSQL = `SELECT admin_user_id
FROM identity_relink_refused($1, $2)`

// IdentityVerification is the link identity_request_verification wrote: its row and its end. The
// token is the caller's; the database saw only its hash.
type IdentityVerification struct {
	ID        uuid.UUID
	ExpiresAt time.Time
}

// RequestIdentityVerification writes an e-mail verification link bound to the IDENTITY behind the
// live, linked session (00072; S16: not to the session). pgx.ErrNoRows: the session is not live or
// not linked, or the address is already verified.
func (d *DB) RequestIdentityVerification(ctx context.Context, sessionHash, tokenHash string) (IdentityVerification, error) {
	var v IdentityVerification
	if err := d.pool.QueryRow(ctx, requestIdentityVerificationSQL, sessionHash, tokenHash).Scan(&v.ID, &v.ExpiresAt); err != nil {
		return IdentityVerification{}, fmt.Errorf("db: request identity verification: %w", err)
	}
	return v, nil
}

// VerifyIdentityEmail spends a verification link -- only with a live session of the SAME identity
// in the clicking browser (R2): sessionHash is that browser's cookie's hash. It reports whether this
// was the identity's first verification (the first opens a first_verify window). pgx.ErrNoRows: the
// link is unknown, spent or expired, or the session is not one of the link's identity.
func (d *DB) VerifyIdentityEmail(ctx context.Context, verificationHash, sessionHash string) (bool, error) {
	var first bool
	if err := d.pool.QueryRow(ctx, verifyIdentityEmailSQL, verificationHash, sessionHash).Scan(&first); err != nil {
		return false, fmt.Errorf("db: verify identity email: %w", err)
	}
	return first, nil
}

// ConfirmWindowRow is one row of identity_confirm_window (00079). The first row is the header
// (TenantID nil): Verified, and -- while the window is open -- Scope and OpenUntil. The rest are the
// list: a membership on it (Refused false) or a row refused in this window (Refused true).
type ConfirmWindowRow struct {
	Verified        bool
	Scope           *string
	OpenUntil       *time.Time
	TenantID        *uuid.UUID
	TenantName      *string
	Role            *string
	Refused         bool
	LastActiveOwner bool
}

// ConfirmWindow reads the confirmation window of the person behind a live, linked session. Empty
// (not an error) when the session is not live or its row is not linked -- an unlinked row has no
// window. It writes nothing.
func (d *DB) ConfirmWindow(ctx context.Context, sessionHash string) ([]ConfirmWindowRow, error) {
	rows, err := d.pool.Query(ctx, confirmWindowSQL, sessionHash)
	if err != nil {
		return nil, fmt.Errorf("db: confirm window: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConfirmWindowRow, error) {
		var c ConfirmWindowRow
		err := r.Scan(&c.Verified, &c.Scope, &c.OpenUntil, &c.TenantID, &c.TenantName, &c.Role, &c.Refused, &c.LastActiveOwner)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("db: confirm window: %w", err)
	}
	return out, nil
}

// KeepAll closes the open confirmation window of the person behind a live, linked session ("Keep
// all", T11): the window's three columns NULL in one statement, one confirm_kept event (00079).
// pgx.ErrNoRows: no live linked session, or no open window.
func (d *DB) KeepAll(ctx context.Context, sessionHash string) error {
	var kept bool
	if err := d.pool.QueryRow(ctx, keepAllSQL, sessionHash).Scan(&kept); err != nil {
		return fmt.Errorf("db: keep all: %w", err)
	}
	if !kept {
		return fmt.Errorf("db: keep all: %w", pgx.ErrNoRows)
	}
	return nil
}

// UnlinkedMembership is the row identity_unlink_backfilled refused, and how many sessions of it it
// revoked (in that business).
type UnlinkedMembership struct {
	AdminUserID     uuid.UUID
	RevokedSessions int
}

// UnlinkMembership refuses the person's membership in tenant from the open window's list (00072):
// a FRESH session (the last five minutes) is required. pgx.ErrNoRows: stale or dead session, closed
// window, or a row not on the list.
func (d *DB) UnlinkMembership(ctx context.Context, freshSessionHash string, tenant uuid.UUID) (UnlinkedMembership, error) {
	var (
		u UnlinkedMembership
		n int32
	)
	if err := d.pool.QueryRow(ctx, unlinkMembershipSQL, freshSessionHash, tenant).Scan(&u.AdminUserID, &n); err != nil {
		return UnlinkedMembership{}, fmt.Errorf("db: unlink membership: %w", err)
	}
	u.RevokedSessions = int(n)
	return u, nil
}

// RelinkMembership takes back a row refused in THIS window (00072, the Undo): a FRESH session is
// required. pgx.ErrNoRows: stale or dead session, closed window, or no such refused row.
func (d *DB) RelinkMembership(ctx context.Context, freshSessionHash string, tenant uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.pool.QueryRow(ctx, relinkMembershipSQL, freshSessionHash, tenant).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("db: relink membership: %w", pgx.ErrNoRows)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: relink membership: %w", err)
	}
	return id, nil
}

// The link of a person's other businesses by e-mail proof (ADR 0031 §5.2's M14-22 note; migration
// 00084): identity_request_verification with the 'link' purpose writes the mailed link, and
// identity_link_by_email spends it with the clicking browser's live session of the same identity.
const requestIdentityLinkSQL = `SELECT verification_id, expires_at
FROM identity_request_verification($1, $2, 'link')`

const linkByEmailSQL = `SELECT linked, first_verification, email
FROM identity_link_by_email($1, $2)`

// RequestIdentityLink writes a link link (purpose 'link', 24 hours) bound to the IDENTITY behind the
// live, linked session -- verified address or not (00084). pgx.ErrNoRows: the session is not live or not
// linked.
func (d *DB) RequestIdentityLink(ctx context.Context, sessionHash, tokenHash string) (IdentityVerification, error) {
	var v IdentityVerification
	if err := d.pool.QueryRow(ctx, requestIdentityLinkSQL, sessionHash, tokenHash).Scan(&v.ID, &v.ExpiresAt); err != nil {
		return IdentityVerification{}, fmt.Errorf("db: request identity link: %w", err)
	}
	return v, nil
}

// LinkedByEmail is identity_link_by_email's row: how many businesses it linked, whether the click was
// the address's first verification, and the identity's REGISTERED address -- the one notice's
// recipient (personal data: never logged).
type LinkedByEmail struct {
	Linked            int
	FirstVerification bool
	Email             string
}

// LinkByEmail spends a link link with the clicking browser's session (sessionHash: that browser's
// cookie's hash) and links every active, unrefused, unlinked row of the identity's address (00084).
// pgx.ErrNoRows: the link is unknown, a verification link, spent or expired, or the session is not one
// of the link's identity.
func (d *DB) LinkByEmail(ctx context.Context, linkHash, sessionHash string) (LinkedByEmail, error) {
	var (
		l LinkedByEmail
		n int32
	)
	if err := d.pool.QueryRow(ctx, linkByEmailSQL, sessionHash, linkHash).Scan(&n, &l.FirstVerification, &l.Email); err != nil {
		return LinkedByEmail{}, fmt.Errorf("db: link by email: %w", err)
	}
	l.Linked = int(n)
	return l, nil
}
