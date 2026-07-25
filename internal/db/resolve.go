package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Tenant resolution (ADR 0002 madde 7) -- the ONLY two lookups that run WITHOUT
// a tenant context, because the tenant is the RESULT of the lookup, not an input.
// They run context-less on the raw pool (NOT through WithTenant / not the
// tenant-scoped store): a tap arrives with only a tag uid or a session token
// hash, and neither carries a tenant. After these resolve, the caller reads the
// tenant off the result and runs everything else inside WithTenant.
//
// WHY HAND-WRITTEN (not sqlc-generated): sqlc v1.28 cannot type a call to a
// `RETURNS TABLE(...)` function -- explicit column lists error ("column does not
// exist") and `SELECT *` yields an untyped interface{}. The two resolve functions
// (00003/00004) must use RETURNS TABLE because they return a SUBSET of their
// table's columns, and the applied migrations are immutable (CLAUDE.md section 6).
// The canonical SQL and the full measurement live in db/queries/resolve.sql; the
// const strings below mirror it verbatim -- keep them in sync by hand.
//
// STRUCTURAL CONTAINMENT (ADR 0002 madde 7): both queries call SECURITY DEFINER
// functions owned by tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER, column-level
// SELECT on exactly these two tables) -- NEVER public.tags / public.sessions
// directly. tappa_app holds EXECUTE on the functions but no cross-tenant SELECT on
// the tables, so the blast radius is those two functions, not the whole DB. These
// accessors deliberately DO NOT call set_config: they establish no tenant context
// (they produce it), which is exactly the narrow resolver access the M1-07 pool.go
// note deferred to M1-08.

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

// getTagByUIDSQL / getEmployeeBySessionHashSQL mirror db/queries/resolve.sql
// verbatim. Real Postgres expands the RETURNS TABLE columns for these explicit
// column lists (proven live); only sqlc's analyzer cannot -- hence hand-written.
const getTagByUIDSQL = `SELECT uid, tenant_id, location_id, aes_key_ref, last_ctr, status
FROM resolve_tag_by_uid($1)`

const getEmployeeBySessionHashSQL = `SELECT id, tenant_id, employee_id, revoked_at
FROM resolve_session_by_token_hash($1)`

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
