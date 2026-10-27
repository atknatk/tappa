package db

import (
	"context"
	"errors"
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
// ID IS A CROSS-TENANT KEY. It travels to OpenIdentitySession, RecordFailedIdentityLogin and (M14-14)
// OpenIdentityReset, and into a log line only under the identity_id key ADR 0031 §5.15 allows -- never
// into a response, a template or an audit row.
type ResolvedIdentity struct {
	ID                  uuid.UUID
	PasswordHash        PasswordHash
	EmailVerifiedAt     *time.Time
	HasActiveMembership bool
	// Email is the identity's REGISTERED address (00075, M14-14): admin_identities.email as stored, not
	// the spelling the caller looked it up with (the two are equal under citext, not byte for byte).
	// It is where a per-identity reset link goes (internal/adminauth binds it to ID through one type);
	// it is personal data and never reaches a log, a response or an audit row (CLAUDE.md §7).
	Email string
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
const resolveIdentityByEmailSQL = `SELECT id, password_hash, email_verified_at, has_active_membership, email
FROM resolve_identity_by_email($1::citext)`

const openIdentitySessionSQL = `SELECT session_id, tenant_id, admin_user_id
FROM identity_open_session($1, $2, $3, $4)`

const identityMembershipsSQL = `SELECT tenant_id, tenant_name, role, suspended, is_current
FROM identity_memberships($1)`

const switchIdentitySessionSQL = `SELECT session_id, tenant_id, admin_user_id
FROM identity_switch_session($1, $2, $3)`

const revokeIdentitySessionsSQL = `SELECT revoked
FROM identity_revoke_sessions($1, $2)`

// The sign-in's three (M14-07, ADR 0031 §5.6): the link two fresh sessions prove (00072), the failed
// identity login's event row (00072, named exception 3), and the decoy the sign-in sends instead of
// identity_open_session whenever it does not open one.
const linkLegacySessionSQL = `SELECT admin_user_id, tenant_id
FROM identity_link_legacy($1, $2)`

const recordFailedIdentityLoginSQL = `SELECT recorded
FROM identity_record_failed_login($1, $2)`

// identitySessionDecoySQL takes a transaction id and returns it, writing no row. Its commit writes a
// commit record but is NOT flushed: a transaction that wrote no other WAL commits asynchronously
// whatever synchronous_commit says, where identity_open_session's commit (four writes before it) is
// flushed. So this is the open's round trip and not its work or its flush; internal/adminauth's wait
// carries the difference (decoy, ADR 0031 §5.6's M14-07 note). It reads nothing and names no
// identity, tenant or session.
const identitySessionDecoySQL = `SELECT pg_catalog.pg_current_xact_id()::text`

// ResolveIdentityByEmail resolves an address to its identity, at most one (admin_identities.email is
// globally UNIQUE). pgx.ErrNoRows when no identity carries the address. It writes nothing.
func (d *DB) ResolveIdentityByEmail(ctx context.Context, email string) (ResolvedIdentity, error) {
	var (
		r      ResolvedIdentity
		digest string
	)
	err := d.pool.QueryRow(ctx, resolveIdentityByEmailSQL, email).Scan(&r.ID, &digest, &r.EmailVerifiedAt, &r.HasActiveMembership, &r.Email)
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

// LinkedMembership is the row identity_link_legacy linked: the legacy membership and its business.
type LinkedMembership struct {
	AdminUserID uuid.UUID
	TenantID    uuid.UUID
}

// LinkLegacySession links the membership behind legacySessionHash to the identity behind
// identitySessionHash -- ADR 0031 §5.7's proof of two credentials: two live sessions, both opened in
// the last five minutes (R3), the first on a linked membership, the second on an unlinked, unrefused
// one of the same address. The legacy session is revoked and the legacy business gets its
// admin.membership.linked row (00072). pgx.ErrNoRows is the one refusal (stale, revoked, refused,
// another address, a membership of the identity already in that business); the function then leaves
// the legacy session live, and the caller revokes it.
func (d *DB) LinkLegacySession(ctx context.Context, identitySessionHash, legacySessionHash string) (LinkedMembership, error) {
	var m LinkedMembership
	if err := d.pool.QueryRow(ctx, linkLegacySessionSQL, identitySessionHash, legacySessionHash).Scan(&m.AdminUserID, &m.TenantID); err != nil {
		return LinkedMembership{}, fmt.Errorf("db: link legacy session: %w", err)
	}
	return m, nil
}

// Identity event names identity_record_failed_login accepts (00072's closed set).
const (
	IdentityEventLoginFailed  = "login_failed"
	IdentityEventLoginLimited = "login_limited"
)

// RecordFailedIdentityLogin writes ONE admin_identity_events row for a sign-in that failed against
// an identity -- ADR 0031 §5.2's named exception 3: keyed by the identity id the sign-in resolved,
// not by a secret, and harmless because it writes that one row (no address, no IP, no tenant) and
// touches nothing else. event is IdentityEventLoginFailed, or IdentityEventLoginLimited for the
// per-identity budget's single over-limit row. It reports whether a row was written: an id no
// identity carries writes nothing and is not an error (the sign-in's padding call relies on that).
func (d *DB) RecordFailedIdentityLogin(ctx context.Context, identityID uuid.UUID, event string) (bool, error) {
	var recorded bool
	err := d.pool.QueryRow(ctx, recordFailedIdentityLoginSQL, identityID, event).Scan(&recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("db: record failed identity login: %w", err)
	}
	return recorded, nil
}

// IdentitySessionDecoy is the round trip a sign-in makes INSTEAD of OpenIdentitySession whenever it
// does not open an identity session (ADR 0031 §5.6, R14, S15): one autocommitted statement on the
// same pool that takes a transaction id and writes no row (its commit record is not flushed -- see
// identitySessionDecoySQL). It carries no argument, so it can open nothing and tell the database
// nothing.
func (d *DB) IdentitySessionDecoy(ctx context.Context) error {
	var xid string
	if err := d.pool.QueryRow(ctx, identitySessionDecoySQL).Scan(&xid); err != nil {
		return fmt.Errorf("db: identity session decoy: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- M14-08 --

// The dual-write's callers (ADR 0031 §5.2, §5.9, §8; M14-08): the account page's password change and
// the legacy reset link of a LINKED row write the identity and every row linked to it (00072), and a
// registration births an identity for a new address (00074, named exception 2). They run whatever
// TAPPA_ADMIN_IDENTITY says: a linked row refuses a direct password write (00069's trigger), so the
// only way to change its password is through the identity.
const identityPasswordForSessionSQL = `SELECT password_hash
FROM identity_password_for_session($1)`

const setIdentityPasswordSQL = `SELECT revoked_other_sessions
FROM identity_set_password($1, $2)`

const consumeIdentityRowResetSQL = `SELECT admin_user_id, tenant_id, reset_id, revoked_sessions
FROM identity_consume_row_reset($1, $2)`

const createIdentityForSignupSQL = `SELECT identity_create_for_signup($1, $2)`

// IdentityPasswordForSession is the digest of the identity behind a live session -- the account
// page's "current password" check. pgx.ErrNoRows when the session is not live or its membership is
// not linked: the caller then compares with the row's own digest, as before M14.
func (d *DB) IdentityPasswordForSession(ctx context.Context, sessionHash string) (PasswordHash, error) {
	var digest string
	if err := d.pool.QueryRow(ctx, identityPasswordForSessionSQL, sessionHash).Scan(&digest); err != nil {
		return PasswordHash{}, fmt.Errorf("db: identity digest for session: %w", err)
	}
	return NewPasswordHash(digest), nil
}

// SetIdentityPassword writes newDigest to the identity behind a live, linked session AND to every
// row linked to it, revokes every other session of those rows in every business (the calling one
// kept) and writes each business its admin.password_changed row (00072). It returns how many of THIS
// business's other sessions it revoked. pgx.ErrNoRows: the session is not live or not linked (the
// caller writes the one row, as before M14), or it was revoked while the call waited for the person's
// lock. newDigest never reaches an error.
func (d *DB) SetIdentityPassword(ctx context.Context, sessionHash, newDigest string) (int, error) {
	var n int32
	if err := d.pool.QueryRow(ctx, setIdentityPasswordSQL, sessionHash, newDigest).Scan(&n); err != nil {
		return 0, fmt.Errorf("db: set identity credential: %w", err)
	}
	return int(n), nil
}

// ConsumedIdentityRowReset is identity_consume_row_reset's row: the link's own membership, its
// business, the link, and how many sessions of THAT business were revoked.
type ConsumedIdentityRowReset struct {
	AdminUserID     uuid.UUID
	TenantID        uuid.UUID
	ResetID         uuid.UUID
	RevokedSessions int
}

// ConsumeIdentityRowReset spends a per-row password_resets link whose row is LINKED (ADR 0015's
// meaning change, ADR 0031 §5.9): the identity and every linked row take newDigest, every session of
// every linked row is revoked, every other live link is cancelled, the mailbox is marked proved and
// a reset-scope confirmation window opens; each business gets its admin.recovery.completed row.
// pgx.ErrNoRows: the link is unknown, spent, cancelled, expired, or its row is not linked, not active
// or refused -- the caller's legacy consumption decides then.
func (d *DB) ConsumeIdentityRowReset(ctx context.Context, resetHash, newDigest string) (ConsumedIdentityRowReset, error) {
	var (
		c ConsumedIdentityRowReset
		n int32
	)
	if err := d.pool.QueryRow(ctx, consumeIdentityRowResetSQL, resetHash, newDigest).Scan(&c.AdminUserID, &c.TenantID, &c.ResetID, &n); err != nil {
		return ConsumedIdentityRowReset{}, fmt.Errorf("db: consume identity row reset: %w", err)
	}
	c.RevokedSessions = int(n)
	return c, nil
}

// CreateIdentityForSignup is the registration's dual-write -- ADR 0031 §5.2's named exception 2
// (migration 00074): inside the registration's OWN transaction (tx, under tenantID's context), it
// births an identity for the new owner's address and links the row to it, or -- when the address is
// already anyone's -- does nothing. It reports whether it linked.
//
// 🔴 tx MUST BE A TOP-LEVEL TRANSACTION (db.WithTenant), NEVER A SAVEPOINT (db.WithTenantIn): the
// function only links a row this transaction wrote, and a row written under a savepoint carries the
// subtransaction's xid -- the call would then refuse silently (S13, measured on 17.10 in 00074).
//
// 🔴 THE ANSWER STAYS IN THE CALLER. It says whether the address was new; ADR 0031 §5.10 allows that
// difference on no surface the flag does not close -- not a response, a log line or an audit row.
func CreateIdentityForSignup(ctx context.Context, tx pgx.Tx, tenantID, adminUserID uuid.UUID) (bool, error) {
	var linked bool
	if err := tx.QueryRow(ctx, createIdentityForSignupSQL, tenantID, adminUserID).Scan(&linked); err != nil {
		return false, fmt.Errorf("db: create identity for signup: %w", err)
	}
	return linked, nil
}

// ---------------------------------------------------------------- M14-14 --

// The per-identity reset (ADR 0031 §5.2, §5.9; M14-14): a request opens at most one live link per
// identity (00072's identity_open_reset, keyed by the id the request resolved -- not a secret, so the
// caller's type binds it to the address the link goes to), an undelivered link is withdrawn by its own
// hash, and a link is spent by its hash (identity_consume_reset, 00075's shape: it returns the
// identity's registered address for the "your password was changed" notice).
const openIdentityResetSQL = `SELECT reset_id, expires_at, issued
FROM identity_open_reset($1, $2, $3)`

const withdrawIdentityResetSQL = `SELECT identity_withdraw_reset($1)`

const consumeIdentityResetSQL = `SELECT identity_id, revoked_sessions, email
FROM identity_consume_reset($1, $2)`

const resolveIdentityResetSQL = `SELECT id, identity_id, expires_at, used_at, cancelled_at
FROM resolve_identity_reset_by_token_hash($1)`

// OpenedIdentityReset is identity_open_reset's row: the identity's live link -- the one just issued
// (Issued) or the one it already held (EM-7C: nothing was written, nothing is to be sent).
type OpenedIdentityReset struct {
	ResetID   uuid.UUID
	ExpiresAt time.Time
	Issued    bool
}

// OpenIdentityReset opens a reset link for an identity, unless it holds a live one (ADR 0031 §5.9,
// EM-7C): tokenHash is the hash of the token the caller just drew. pgx.ErrNoRows: no such identity.
// The hash never reaches an error.
func (d *DB) OpenIdentityReset(ctx context.Context, identityID uuid.UUID, tokenHash string, expiresAt time.Time) (OpenedIdentityReset, error) {
	var o OpenedIdentityReset
	if err := d.pool.QueryRow(ctx, openIdentityResetSQL, identityID, tokenHash, expiresAt).Scan(&o.ResetID, &o.ExpiresAt, &o.Issued); err != nil {
		return OpenedIdentityReset{}, fmt.Errorf("db: open identity reset: %w", err)
	}
	return o, nil
}

// WithdrawIdentityReset retires an identity link the caller issued and could not deliver. It reports
// whether a link was retired; a link already spent, cancelled or unknown is false, not an error.
func (d *DB) WithdrawIdentityReset(ctx context.Context, tokenHash string) (bool, error) {
	var withdrawn bool
	if err := d.pool.QueryRow(ctx, withdrawIdentityResetSQL, tokenHash).Scan(&withdrawn); err != nil {
		return false, fmt.Errorf("db: withdraw identity reset: %w", err)
	}
	return withdrawn, nil
}

// ConsumedIdentityReset is identity_consume_reset's row: whose link it was, how many sessions it
// revoked in every business, and the identity's registered address (personal data: the notice's
// recipient and nothing else).
type ConsumedIdentityReset struct {
	IdentityID      uuid.UUID
	RevokedSessions int
	Email           string
}

// ConsumeIdentityReset spends a per-identity link: the identity and every linked row take newDigest,
// every session of the person is revoked, the mailbox is marked proved and a reset-scope confirmation
// window opens; each business gets its admin.recovery.completed row. pgx.ErrNoRows: the link is
// unknown, spent, cancelled or expired.
func (d *DB) ConsumeIdentityReset(ctx context.Context, resetHash, newDigest string) (ConsumedIdentityReset, error) {
	var (
		c ConsumedIdentityReset
		n int32
	)
	if err := d.pool.QueryRow(ctx, consumeIdentityResetSQL, resetHash, newDigest).Scan(&c.IdentityID, &n, &c.Email); err != nil {
		return ConsumedIdentityReset{}, fmt.Errorf("db: consume identity reset: %w", err)
	}
	c.RevokedSessions = int(n)
	return c, nil
}

// ResolvedIdentityReset is resolve_identity_reset_by_token_hash's row: a per-identity link and the
// three facts that decide whether it could be spent -- carried for the refusal's trail, never applied.
type ResolvedIdentityReset struct {
	ID          uuid.UUID
	IdentityID  uuid.UUID
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CancelledAt *time.Time
}

// GetIdentityResetByTokenHash resolves a per-identity link by its hash (ADR 0002 md.7). pgx.ErrNoRows
// when no identity link carries it.
func (d *DB) GetIdentityResetByTokenHash(ctx context.Context, tokenHash string) (ResolvedIdentityReset, error) {
	var r ResolvedIdentityReset
	if err := d.pool.QueryRow(ctx, resolveIdentityResetSQL, tokenHash).Scan(&r.ID, &r.IdentityID, &r.ExpiresAt, &r.UsedAt, &r.CancelledAt); err != nil {
		return ResolvedIdentityReset{}, fmt.Errorf("db: resolve identity reset: %w", err)
	}
	return r, nil
}

// ---------------------------------------------------------------- M14-17 --

// The verified registration (ADR 0031 §5.10, M14-17; migration 00078): POST /signup opens a DRAFT keyed
// by the token it just drew (identity_open_signup_claim -- writes no identity, S11), the link's holder
// reads it back by the token's hash (resolve_signup_claim_by_token_hash, md.7: the digest and the draft
// return only against the claim secret -- S12, the 2026-10-26 user decision), and the confirmation
// consumes it inside the registration's own transaction (identity_claim_signup). The sweep is the
// table's one delete path (R17).
const openSignupClaimSQL = `SELECT identity_open_signup_claim($1, $2::citext, $3::jsonb, $4, $5)`

const resolveSignupClaimSQL = `SELECT id, email, password_hash, expires_at, used_at, draft
FROM resolve_signup_claim_by_token_hash($1)`

const claimSignupSQL = `SELECT admin_user_id
FROM identity_claim_signup($1, $2, $3)`

const sweepSignupClaimsSQL = `SELECT identity_sweep_signup_claims()`

// OpenSignupClaim writes one registration draft: tokenHash is the hash of the token the caller just drew,
// email the address the link goes to, draft the closed-key JSON internal/domain/signup encodes, digest the
// bcrypt of the chosen password. pgx.ErrNoRows when the function refused it (a value the table would not
// take, or an address that is not printable ASCII) -- nothing was written. No argument reaches an error.
func (d *DB) OpenSignupClaim(ctx context.Context, tokenHash, email string, draft []byte, digest string, expiresAt time.Time) (uuid.UUID, error) {
	var id *uuid.UUID
	if err := d.pool.QueryRow(ctx, openSignupClaimSQL, tokenHash, email, string(draft), digest, expiresAt).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("db: open signup claim: %w", err)
	}
	if id == nil {
		return uuid.Nil, fmt.Errorf("db: open signup claim: refused: %w", pgx.ErrNoRows)
	}
	return *id, nil
}

// ResolvedSignupClaim is resolve_signup_claim_by_token_hash's row: the draft a link names, the address it
// was sent to, the digest of the password the form chose (compared in Go, never in SQL -- S12) and the
// two facts that decide whether it can still be confirmed. Email and Draft are personal data: never a
// log line, an error or an audit row.
type ResolvedSignupClaim struct {
	ID           uuid.UUID
	Email        string
	PasswordHash PasswordHash
	ExpiresAt    time.Time
	UsedAt       *time.Time
	Draft        []byte
}

// GetSignupClaimByTokenHash resolves a registration link by its hash (ADR 0002 md.7). pgx.ErrNoRows when
// no draft carries it (never issued, or swept). It writes nothing: the confirmation page's GET reads
// through it and spends nothing (R19).
func (d *DB) GetSignupClaimByTokenHash(ctx context.Context, tokenHash string) (ResolvedSignupClaim, error) {
	var (
		r      ResolvedSignupClaim
		digest string
	)
	if err := d.pool.QueryRow(ctx, resolveSignupClaimSQL, tokenHash).Scan(&r.ID, &r.Email, &digest, &r.ExpiresAt, &r.UsedAt, &r.Draft); err != nil {
		return ResolvedSignupClaim{}, fmt.Errorf("db: resolve signup claim: %w", err)
	}
	r.PasswordHash = NewPasswordHash(digest)
	return r, nil
}

// ClaimSignup consumes a registration draft INSIDE the registration's own transaction (tx, under
// tenantID's context, the tenant this transaction just created) and writes the owner membership -- for
// a new address with a new identity (sessionHash nil: the caller compared the re-entered password with
// the draft's digest first), for an existing identity through its own live session (sessionHash set).
// It returns the owner row's id. pgx.ErrNoRows is the one refusal (a spent, expired or unknown link, a
// business that is not this transaction's or already has a member, a draft that does not match it, a
// session that is not the address's person, a concurrent birth of the address); the caller rolls back.
//
// 🔴 tx MUST BE A TOP-LEVEL TRANSACTION (db.WithTenant, never db.WithTenantIn): the function requires the
// tenant row's xmin to be this transaction's, which a savepoint's insert is not (S13).
//
// 🔴 ONE CALLER: internal/adminauth's claim proof (TestSignupClaimProof_HasItsProducersAndOneConsumer) --
// the Go type that exists only after the password matched or with a session cookie in hand (S12).
func ClaimSignup(ctx context.Context, tx pgx.Tx, claimHash string, tenantID uuid.UUID, sessionHash *string) (uuid.UUID, error) {
	var admin uuid.UUID
	if err := tx.QueryRow(ctx, claimSignupSQL, claimHash, tenantID, sessionHash).Scan(&admin); err != nil {
		return uuid.Nil, fmt.Errorf("db: claim signup: %w", err)
	}
	return admin, nil
}

// SweepSignupClaims deletes the expired and the used registration drafts -- the table's one delete path
// (ADR 0031 §5.1, R17) -- and returns how many went.
func (d *DB) SweepSignupClaims(ctx context.Context) (int, error) {
	var n int32
	if err := d.pool.QueryRow(ctx, sweepSignupClaimsSQL).Scan(&n); err != nil {
		return 0, fmt.Errorf("db: sweep signup claims: %w", err)
	}
	return int(n), nil
}

// ---------------------------------------------------------------- M14-15 --

// The administrator invitation (ADR 0031 §5.11, §5.2; M14-15, migration 00077): the md.7 resolver the
// acceptance page asks "is this link still usable", and the two acceptances -- keyed by the
// invitation token's hash, the first also by a live session's hash (the only way to the identity).
// Neither acceptance gives a session; each answers the membership it made and its business, or
// nothing (pgx.ErrNoRows). No hash ever reaches an error.
const resolveAdminInviteSQL = `SELECT id, tenant_id, role, expires_at, used_at, cancelled_at
FROM resolve_admin_invite_by_token_hash($1)`

const acceptAdminInviteSQL = `SELECT admin_user_id, tenant_id
FROM identity_accept_invite($1, $2)`

const acceptAdminInviteNewSQL = `SELECT admin_user_id, tenant_id
FROM identity_accept_invite_new($1, $2)`

// ResolvedAdminInvite is resolve_admin_invite_by_token_hash's row: an invitation and the three facts
// that decide whether it could be accepted. It carries no address, no name and no inviter.
type ResolvedAdminInvite struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Role        string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CancelledAt *time.Time
}

// AcceptedAdminInvite is an acceptance's row: the membership the invitation made (or relinked) and
// its business.
type AcceptedAdminInvite struct {
	AdminUserID uuid.UUID
	TenantID    uuid.UUID
}

// GetAdminInviteByTokenHash resolves an invitation by its token's hash (ADR 0002 md.7; it writes
// nothing). pgx.ErrNoRows when no invitation carries it.
func (d *DB) GetAdminInviteByTokenHash(ctx context.Context, tokenHash string) (ResolvedAdminInvite, error) {
	var r ResolvedAdminInvite
	if err := d.pool.QueryRow(ctx, resolveAdminInviteSQL, tokenHash).Scan(&r.ID, &r.TenantID, &r.Role, &r.ExpiresAt, &r.UsedAt, &r.CancelledAt); err != nil {
		return ResolvedAdminInvite{}, fmt.Errorf("db: resolve admin invite: %w", err)
	}
	return r, nil
}

// AcceptAdminInvite accepts an invitation with the live session of the identity whose address it
// names (identity_accept_invite): the membership is born (or the address's refused row relinked), the
// invitation spent, the mailbox proved. pgx.ErrNoRows: the invitation is not usable, the session is not
// a live linked one, its person is not the invitation's address, or the address is already in that
// business.
func (d *DB) AcceptAdminInvite(ctx context.Context, inviteHash, sessionHash string) (AcceptedAdminInvite, error) {
	var a AcceptedAdminInvite
	if err := d.pool.QueryRow(ctx, acceptAdminInviteSQL, inviteHash, sessionHash).Scan(&a.AdminUserID, &a.TenantID); err != nil {
		return AcceptedAdminInvite{}, fmt.Errorf("db: accept admin invite: %w", err)
	}
	return a, nil
}

// AcceptAdminInviteNew accepts an invitation for an address with NO identity, with the digest of the
// password the invitee chose (identity_accept_invite_new): the identity is born verified, with its
// membership. pgx.ErrNoRows: the invitation is not usable, the address already has an identity or is
// already in that business, or the digest is not a bcrypt digest.
func (d *DB) AcceptAdminInviteNew(ctx context.Context, inviteHash, newDigest string) (AcceptedAdminInvite, error) {
	var a AcceptedAdminInvite
	if err := d.pool.QueryRow(ctx, acceptAdminInviteNewSQL, inviteHash, newDigest).Scan(&a.AdminUserID, &a.TenantID); err != nil {
		return AcceptedAdminInvite{}, fmt.Errorf("db: accept admin invite (new sign-in): %w", err)
	}
	return a, nil
}

// --------------------------------------------------------------- M14-16b --

// The owners of a business who can still get a session (ADR 0031 §5.2's note of 2026-10-27; migration
// 00081): resolve_admin_owners_by_tenant, tappa_resolver's, read-only. M14-16's last-owner rule counts
// these and not every active owner row: a membership the person refused keeps role='owner',
// status='active' with unlinked_at stamped, which tappa_app cannot read (R16).
const adminOwnersByTenantSQL = `SELECT admin_user_id
FROM resolve_admin_owners_by_tenant($1)`

// SignInOwners returns the active owner memberships of tenantID that can still get a session, in id
// order (at most 200). Ids only: a refused owner row is told apart by its absence.
//
// 🔴 tx IS THE CALLER'S TENANT TRANSACTION (db.WithTenant): the function answers only for the tenant
// the transaction's app.tenant_id names -- any other tenantID is no rows, never another business's
// owners. Called after the owners' row lock (internal/adminauth.Memberships), it reads under it:
// unlinked_at cannot change on a locked row (an unlink and a relink update the row).
func SignInOwners(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, adminOwnersByTenantSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("db: owners who can sign in: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("db: owners who can sign in: %w", err)
	}
	return ids, nil
}
