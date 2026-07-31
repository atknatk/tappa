package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Tenant resolution (ADR 0002 madde 7) -- the ONLY lookups that run WITHOUT a
// tenant context, because the tenant is the RESULT of the lookup, not an input.
// They run context-less on the raw pool (NOT through WithTenant / not the
// tenant-scoped store): a tap arrives with only a tag uid or a session token
// hash, an activation with only an invite code, and none of the three carries a
// tenant. After these resolve, the caller reads the tenant off the result and
// runs everything else inside WithTenant.
//
// There are THREE of them (M5-02 added the invite one; ADR 0002 madde 7 was
// written when there were two and now carries a dated update note). The count is
// not the guarantee -- the five normative constraints are, and each accessor here
// meets all five: key-only input, at most one row (a globally UNIQUE index, not
// a promise made by the function body), a fixed column list with no SELECT *
// surface, EXECUTE granted to tappa_app alone with no cross-tenant SELECT on the
// tables, and no naive "show rows when the context is NULL" RLS branch anywhere.
//
// WHY HAND-WRITTEN (not sqlc-generated): sqlc v1.28 cannot type a call to a
// `RETURNS TABLE(...)` function -- explicit column lists error ("column does not
// exist") and `SELECT *` yields an untyped interface{}. The three resolve
// functions (00003/00004/00009) must use RETURNS TABLE because they return a
// SUBSET of their table's columns, and applied migrations are immutable
// (CLAUDE.md section 6). The canonical SQL and the full measurement live in
// db/queries/resolve.sql; the const strings below mirror it verbatim -- keep them
// in sync by hand (the column contract is pinned by TestResolveColumns_MatchSchema
// and TestResolveInviteColumns_MatchSchema).
//
// STRUCTURAL CONTAINMENT (ADR 0002 madde 7): every query here calls a SECURITY
// DEFINER function owned by tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER,
// column-level SELECT on exactly these three tables) -- NEVER public.tags /
// public.sessions / public.employee_invites directly. tappa_app holds EXECUTE on
// the functions but no cross-tenant SELECT on the tables, so the blast radius is
// those three functions, not the whole DB. These accessors deliberately DO NOT
// call set_config: they establish no tenant context (they produce it), which is
// exactly the narrow resolver access the M1-07 pool.go note deferred to M1-08.

// ResolvedTag is the fixed column set returned by resolve_tag_by_uid: the state a
// tap needs to run SUN verification and set the tenant context. aes_key_ref is
// the KEK-WRAPPED key (CLAUDE.md section 4.7 -- useless without the KEK); last_ctr
// is state only and is NEVER compared read-then-write (the atomic advance is
// store.AdvanceTagCounter, section 4.4).
type ResolvedTag struct {
	UID        string
	TenantID   uuid.UUID
	LocationID uuid.UUID
	AESKeyRef  []byte
	LastCtr    int32
	Status     string
}

// ResolvedSession is the fixed column set returned by resolve_session_by_token_hash.
// RevokedAt is carried but NOT filtered -- the revocation decision lives in the
// domain (CLAUDE.md section 5), the data layer only carries the truth. Nil means
// the session is not revoked.
type ResolvedSession struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	RevokedAt  *time.Time
}

// ResolvedInvite is the fixed column set returned by resolve_invite_by_code_hash
// (migration 00009, M5-02): who an activation code belongs to, plus the two facts
// that decide whether it may still be used. ExpiresAt and UsedAt are CARRIED, not
// applied -- the resolver deliberately does not filter on them, so a consumed or
// expired invite still resolves and the caller can tell "replay" from "unknown
// code" instead of losing that distinction (00009 comment; the same principle as
// ResolvedSession.RevokedAt). UsedAt nil means not yet consumed.
//
// TOCTOU WARNING (CLAUDE.md section 4.4, the tags.last_ctr trap in another
// costume): UsedAt read here MUST NOT be compared and then written. Consumption
// is store.ConsumeInviteAndActivate -- one atomic UPDATE whose WHERE carries the
// state test. Reading UsedAt to decide whether to consume is a race that yields
// two activations for one code.
//
// ID IS FOR LOGGING AND JOINS, NOT FOR CONSUMING. Consumption is keyed by the CODE
// HASH on purpose (db/queries/invites.sql), so that possessing the code -- not
// merely knowing a row id, which any tenant-scoped caller can list -- is what
// consumes an invite. Nothing in this repo consumes by id; there is no such query.
//
// The invite CODE never appears in this struct: only its hash is an input, and
// not even the hash is returned (section 4.7).
type ResolvedInvite struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	ExpiresAt  time.Time
	UsedAt     *time.Time
}

// The SQL constants mirror db/queries/resolve.sql verbatim. Real Postgres expands
// the RETURNS TABLE columns for these explicit column lists (proven live); only
// sqlc's analyzer cannot -- hence hand-written.
const getTagByUIDSQL = `SELECT uid, tenant_id, location_id, aes_key_ref, last_ctr, status
FROM resolve_tag_by_uid($1)`

const getEmployeeBySessionHashSQL = `SELECT id, tenant_id, employee_id, revoked_at
FROM resolve_session_by_token_hash($1)`

const getInviteByCodeHashSQL = `SELECT id, tenant_id, employee_id, expires_at, used_at
FROM resolve_invite_by_code_hash($1)`

// GetTagByUID resolves a wall-plaque uid to its tag WITHOUT a tenant context.
// Returns pgx.ErrNoRows when the uid is unknown (the caller rejects the tap).
// uid is the 14-hex-char NTAG 424 DNA UID.
func (d *DB) GetTagByUID(ctx context.Context, uid string) (ResolvedTag, error) {
	var t ResolvedTag
	err := d.pool.QueryRow(ctx, getTagByUIDSQL, uid).Scan(
		&t.UID, &t.TenantID, &t.LocationID, &t.AESKeyRef, &t.LastCtr, &t.Status,
	)
	if err != nil {
		// Wrap for context but keep it unwrappable so callers can errors.Is it
		// against pgx.ErrNoRows (unknown tag -> reject).
		return ResolvedTag{}, fmt.Errorf("db: resolve tag by uid: %w", err)
	}
	return t, nil
}

// GetEmployeeBySessionHash resolves a session token hash to its session (id,
// tenant, employee, revocation) WITHOUT a tenant context. Returns pgx.ErrNoRows
// when the hash is unknown (the caller shows the activation page, writes nothing).
func (d *DB) GetEmployeeBySessionHash(ctx context.Context, tokenHash string) (ResolvedSession, error) {
	var s ResolvedSession
	err := d.pool.QueryRow(ctx, getEmployeeBySessionHashSQL, tokenHash).Scan(
		&s.ID, &s.TenantID, &s.EmployeeID, &s.RevokedAt,
	)
	if err != nil {
		// Message deliberately omits the word "token" and never the value: the
		// hash argument is not interpolated, and %w wraps only the DB error.
		return ResolvedSession{}, fmt.Errorf("db: resolve session by hash: %w", err)
	}
	return s, nil
}

// GetInviteByCodeHash resolves an activation code hash to its invite (id, tenant,
// employee, expiry, consumption) WITHOUT a tenant context -- an activation link
// carries only a code, so the tenant is the RESULT. Returns pgx.ErrNoRows when the
// hash is unknown.
//
// It answers "whose invite is this and what state is it in", NOT "may this
// activation proceed". The caller establishes the tenant context from the result
// and then calls store.ConsumeInviteAndActivate, whose single atomic UPDATE is the only thing
// that may conclude the code was still usable (section 4.4 shape).
func (d *DB) GetInviteByCodeHash(ctx context.Context, codeHash string) (ResolvedInvite, error) {
	var i ResolvedInvite
	err := d.pool.QueryRow(ctx, getInviteByCodeHashSQL, codeHash).Scan(
		&i.ID, &i.TenantID, &i.EmployeeID, &i.ExpiresAt, &i.UsedAt,
	)
	if err != nil {
		// The message names neither the code nor its hash: the argument is not
		// interpolated and %w wraps only the DB error (section 4.7).
		return ResolvedInvite{}, fmt.Errorf("db: resolve invite by hash: %w", err)
	}
	return i, nil
}
