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
// There are FIVE of them (M5-02 added the invite one, M6-01 phase A the two admin
// ones; ADR 0002 madde 7 was written when there were two and carries dated update
// notes). The count is not the guarantee -- the five normative constraints are,
// and each accessor here meets them: key-only input, a bounded result underwritten
// by a UNIQUE index rather than by the function body, a fixed column list with no
// SELECT * surface, EXECUTE granted to tappa_app alone with no cross-tenant SELECT
// on the tables, and no naive "show rows when the context is NULL" RLS branch
// anywhere.
//
// ONE OF THE FIVE CONSTRAINTS CHANGES SHAPE FOR GetAdminByEmail, and it is called
// out rather than glossed: constraint (ii) is "at most ONE row", which holds for
// the other four because their key is GLOBALLY unique. An admin email is unique
// only WITHIN a tenant (the same person may own two businesses -- the user decision
// behind migration 00011), so that lookup returns N rows. The bound is still
// structural, just different: at most one row PER TENANT, from the partial UNIQUE
// index (tenant_id, email), hence at most count(tenants) rows and never two rows
// with the same tenant_id.
//
// WHY HAND-WRITTEN (not sqlc-generated): sqlc v1.28 cannot type a call to a
// `RETURNS TABLE(...)` function -- explicit column lists error ("column does not
// exist") and `SELECT *` yields an untyped interface{}. Re-measured against
// 00011's resolve_admin_by_email in M6-01: identical behaviour. The resolve
// functions (00003/00004/00009/00011) must use RETURNS TABLE because they return a
// SUBSET of their table's columns, and applied migrations are immutable
// (CLAUDE.md section 6). The canonical SQL and the full measurement live in
// db/queries/resolve.sql; the const strings below mirror it verbatim -- keep them
// in sync by hand (the column contract is pinned by TestResolveColumns_MatchSchema,
// TestResolveInviteColumns_MatchSchema and TestResolveAdmin_SecurityDefinerProperties).
//
// STRUCTURAL CONTAINMENT (ADR 0002 madde 7): every query here calls a SECURITY
// DEFINER function owned by tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER,
// column-level SELECT on exactly these five tables) -- NEVER public.tags /
// public.sessions / public.employee_invites / public.admin_users /
// public.admin_sessions directly. tappa_app holds EXECUTE on the functions but no
// cross-tenant SELECT on the tables, so the blast radius is those functions, not
// the whole DB. These accessors deliberately DO NOT call set_config: they establish
// no tenant context (they produce it), which is exactly the narrow resolver access
// the M1-07 pool.go note deferred to M1-08.

// ResolvedTag is the fixed column set returned by resolve_tag_by_uid: the state a
// tap needs to run SUN verification and set the tenant context. aes_key_ref is
// the KEK-WRAPPED key (CLAUDE.md section 4.7 -- useless without the KEK); last_ctr
// is state only and is NEVER compared read-then-write (the atomic advance is
// store.AdvanceTagCounter, section 4.4).
type ResolvedTag struct {
	UID      string
	TenantID uuid.UUID
	// LocationID is the wall this plaque is mounted at, or nil for a plaque that is
	// NOT MOUNTED (inventory/stock -- migration 00013's model).
	//
	// 🔴 IT IS A POINTER BECAUSE THE COLUMN IS NULLABLE AND THE DRIVER WILL NOT SAY
	// SO. Measured on the pinned driver (pgx v5.10.0): `SELECT NULL::uuid` scanned
	// into a uuid.UUID gives err=nil and the value
	// 00000000-0000-0000-0000-000000000000, while the same scan into [16]byte
	// FAILS. So with a flat uuid.UUID a stock plaque arrived here looking like a
	// plaque mounted at "location zero", and nothing anywhere could tell the two
	// apart. Of the five hand-written resolvers this is the ONLY struct with a
	// column that can be NULL, which is why it is the only one that needed this.
	//
	// ⚠️ IT WAS FLAT UNTIL M6-06 PHASE B AND THE COMMENT HERE CARRIED THE MEANING
	// INSTEAD -- "uuid.Nil here means NOT MOUNTED". That is the shape this
	// repository keeps paying for: a value that is silently valid, comparable and
	// wrong, with a paragraph asking the next reader to remember. The type now says
	// it, and a caller that wants a venue id has to decide what to do about nil.
	//
	// WHAT THE CHANGE DID NOT ALTER, measured rather than assumed: the tap path
	// behaves identically. A nil here reaches GetLocationForTap as "no location",
	// which answers no row, which the caller already handles as "no evidence to
	// judge against" -- and section 5 row 1 refuses the tap before that matters,
	// because sys:tag-not-active asks for `active` and every other status is denied.
	LocationID *uuid.UUID
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
	// CancelledAt is when this invitation was RETIRED WITHOUT BEING USED
	// (migration 00012), nil while it is still spendable.
	//
	// 🔴 IT IS A SECOND, INDEPENDENT FACT AND IS NEVER READ AS UsedAt. "Somebody
	// spent this code" and "this code was retired because a newer one was issued"
	// are different events with different investigations behind them, and
	// db/queries/invites.sql forbids collapsing them. Carried and NOT applied, for
	// the reason the paragraph above gives about ExpiresAt and UsedAt: a cancelled
	// invitation still RESOLVES, so a request carrying one can be told apart from an
	// unknown code and written to audit_log with a tenant to attribute it to.
	//
	// THE SAME TOCTOU WARNING APPLIES. Reading this and then deciding to consume is
	// the tags.last_ctr trap; the authority is ConsumeInviteAndActivate, whose WHERE
	// carries `cancelled_at IS NULL`.
	CancelledAt *time.Time
}

// ResolvedAdmin is one candidate identity for a login attempt: the columns
// resolve_admin_by_email returns (migration 00011). There may be SEVERAL of them
// for one email -- one per business the person is an admin of -- which is why
// GetAdminByEmail returns a slice and why the tenant PICKER exists.
//
// PasswordHash is the digest the caller compares the submitted password against.
// It is the only reason this lookup runs without a tenant context: there is no
// context to read it in until authentication has succeeded.
//
// IT IS NOT A string, and that is load-bearing rather than stylistic: it is the
// redacting PasswordHash type (passwordhash.go), the third application of the
// pattern internal/session.Token and internal/invite.Code already carry. As a bare
// string, six ordinary rendering paths printed the owner's digest verbatim
// (%+v · %v over a slice · %#v · fmt.Errorf · through an unexported field · slog),
// and a leaked digest is an OFFLINE cracking target with no rate limit and no
// revocation. The single way to read it in the clear is
// RevealForPasswordComparison, which greps to one intended call site: phase B's KDF
// comparison. Everything else -- every verb, slog, encoding/json, error wrapping --
// renders a placeholder (measured both ways in resolve_leak_external_test.go).
//
// Status is CARRIED, not applied: 'disabled' rows resolve like any other, and the
// caller decides. Refusing them here would make a disabled account indistinguishable
// from an unknown email at the point where the attempt should be attributable
// (section 4.6). The caller must still refuse them -- and CreateAdminSession refuses
// independently, in SQL, so forgetting fails closed.
//
// Deliberately absent: role (authorization, read in-context once the tenant is
// known), full_name, email (it is the input), created_at, last_login_at.
//
// ⚠️ THIS STRUCT IS NO LONGER VALUE-COMPARABLE IN THE USEFUL SENSE. PasswordHash
// holds a pointer, so `==` on two ResolvedAdmin values scanned from the SAME row is
// false. Consequences are fail-CLOSED (a false non-match) and nothing in this repo
// compares them; identity is decided on ID/TenantID, which are plain uuids.
type ResolvedAdmin struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	PasswordHash PasswordHash
	Status       string
}

// ResolvedAdminSession is the fixed column set returned by
// resolve_admin_session_by_token_hash (migration 00011): the twin of
// ResolvedSession over the SEPARATE admin table. An employee cookie can never
// resolve here and an admin cookie can never resolve through
// resolve_session_by_token_hash -- two tables, two functions (M1-11).
//
// RevokedAt is carried but NOT filtered, exactly like ResolvedSession.RevokedAt:
// the caller must be able to tell a revoked session (an event worth auditing) from
// an unknown token (noise). Nil means live. The authoritative liveness test is
// store.TouchAdminSession, which additionally refuses a disabled admin.
type ResolvedAdminSession struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
	RevokedAt   *time.Time
}

// The SQL constants mirror db/queries/resolve.sql verbatim. Real Postgres expands
// the RETURNS TABLE columns for these explicit column lists (proven live); only
// sqlc's analyzer cannot -- hence hand-written.
const getTagByUIDSQL = `SELECT uid, tenant_id, location_id, aes_key_ref, last_ctr, status
FROM resolve_tag_by_uid($1)`

const getEmployeeBySessionHashSQL = `SELECT id, tenant_id, employee_id, revoked_at
FROM resolve_session_by_token_hash($1)`

const getInviteByCodeHashSQL = `SELECT id, tenant_id, employee_id, expires_at, used_at, cancelled_at
FROM resolve_invite_by_code_hash($1)`

// $1::citext is EXPLICIT, not required -- the earlier comment here claimed the cast
// was necessary and the claim was measured false. Mutation: this const was changed
// to a bare $1 and the whole internal/db package stayed GREEN (every
// TestGetAdminByEmail* case included), because pgx sends an untyped text parameter
// and Postgres coerces it to the function's citext parameter itself. The cast is
// kept as hygiene: it states at the call site which type the argument is read as,
// and it keeps a future overload or a change in driver type inference from quietly
// selecting a different one. The case-insensitive comparison happens INSIDE the
// function, where the citext operator is schema-qualified (00011).
const getAdminByEmailSQL = `SELECT id, tenant_id, password_hash, status
FROM resolve_admin_by_email($1::citext)`

const getAdminSessionByTokenHashSQL = `SELECT id, tenant_id, admin_user_id, revoked_at
FROM resolve_admin_session_by_token_hash($1)`

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
		&i.ID, &i.TenantID, &i.EmployeeID, &i.ExpiresAt, &i.UsedAt, &i.CancelledAt,
	)
	if err != nil {
		// The message names neither the code nor its hash: the argument is not
		// interpolated and %w wraps only the DB error (section 4.7).
		return ResolvedInvite{}, fmt.Errorf("db: resolve invite by hash: %w", err)
	}
	return i, nil
}

// GetAdminByEmail resolves a panel login email to EVERY admin identity registered
// under it, across tenants, WITHOUT a tenant context -- one login page, one email,
// and the tenant is the RESULT (migration 00011, the user decision of 2026-08-02).
//
// It returns a SLICE, not a row, and that is the whole design: an email registered
// in two businesses yields two candidates, and the caller -- after verifying the
// password -- either proceeds (exactly one match) or renders the "which business?"
// picker. An empty slice and a nil error mean "no such email"; unlike the other
// four resolvers this is NOT pgx.ErrNoRows, because zero is an ordinary cardinality
// for a set-returning lookup.
//
// OBLIGATIONS THIS FUNCTION CANNOT DISCHARGE, stated where the caller will see
// them. They are the M6-01 phase B handler's, and the canonical numbered list is
// 00011's "PHASE B OBLIGATION"; four of its five are about THIS function and are
// repeated below with 00011's numbers. (Obligation 3 -- rate limiting and an
// audit_log row for failures -- is a property of the endpoint, not of this lookup,
// so it is named there and not here.)
//
//   - OBLIGATION 1 -- EMAIL ENUMERATION. The empty slice is an oracle: it answers
//     "is this address registered?" without a password. The HTTP layer must make
//     the three outcomes -- unknown email, wrong password, disabled admin --
//     indistinguishable in status, body and wording.
//   - OBLIGATION 2 -- TIMING. When this returns nothing, the caller must still
//     perform a DUMMY bcrypt comparison against a fixed hash. bcrypt is deliberately
//     slow; skipping it answers the same question the body refused to, in
//     milliseconds.
//   - OBLIGATION 4 -- COST AMPLIFICATION, a DoS bound and not a timing one. The
//     slice length is the number of bcrypt comparisons
//     the caller is about to run, and the expensive half is entirely on the
//     caller's side. Measured: an address planted in 500 tenants resolves 500 rows
//     in ~0.9-1.2 ms warm (2.8 ms cold) -- the database is NOT the bottleneck --
//     while a cost-10 bcrypt comparison is ~60-100 ms. One unauthenticated
//     POST /login therefore buys ~30-50 s of CPU: ~500x amplification off a single
//     credential-less request. Not reachable today (no application path creates a
//     tenant: no CreateTenant/InsertTenant query exists, and the only INSERT INTO
//     tenants lives in test/fixtures/seed.sql and test helpers), but M7-02's
//     sign-up wizard is PUBLIC and RLS does not stop a tenant
//     from writing ANY email into its OWN admin_users, so from M7-02 the attacker
//     controls the row count. The handler must bound the work: cap the candidates
//     one request will verify, stop at the first match, or require email
//     verification before an admin row is resolvable -- chosen in phase B against
//     phase B's own measurement, not decided here or in 00011. READ THIS TOGETHER
//     WITH OBLIGATION 5: one of those three options collides with it.
//   - OBLIGATION 5 -- CANDIDATE <-> PASSWORD BINDING. THE SESSION IS ISSUED ONLY TO
//     THE CANDIDATE WHOSE HASH MATCHED, AND THE PICKER OFFERS ONLY MATCHED
//     CANDIDATES. This is the heaviest obligation on the list and the only one that
//     is a cross-tenant AUTHENTICATION BYPASS (section 4.5) rather than an
//     enumeration or a DoS bound, and NOTHING in the data layer can enforce it:
//     this function returns a SET, and CreateAdminSession never sees a password --
//     all it requires is that (admin_user_id, tenant_id) be internally consistent,
//     and the pair a picker hands back IS a consistent pair of real database values,
//     so every other guard is satisfied.
//     THE ATTACK, from M7-02 on: the attacker signs up their own tenant through the
//     public wizard and writes the VICTIM'S EMAIL plus a password THEY know into
//     their OWN admin_users (RLS permits it -- it is their tenant). Logging in with
//     that email resolves TWO candidates; the password matches THEIR row; a picker
//     that lists both then offers the victim's business, and choosing it issues a
//     session in the victim's tenant. Measured live on this schema:
//     "only AttackerCo's hash verified, picker offers VictimCo" ->
//     INSERT ... WHERE a.id=<attacker admin> AND a.tenant_id=<VICTIM> AND
//     a.status='active' -> INSERT 0 1, i.e. a session stamped with the VICTIM's
//     tenant; every subsequent request then passes TouchAdminSession -> UPDATE 1,
//     role=owner, full_name='Victim Owner'.
//     ⚠️ TENSION WITH OBLIGATION 4, WRITTEN DOWN BECAUSE THE TWO PULL OPPOSITE WAYS
//     AND NOBODY HAD SAID SO: "stop at the first match" reduces the bcrypt DoS, but
//     implemented as "stop at the first match AND show every candidate in the
//     picker" it IS this bypass exactly. The two obligations must be read together:
//     whatever bounds the work, the set the picker offers must never be wider than
//     the set whose hash actually verified.
//
// The email is not logged here and is not interpolated into any error.
func (d *DB) GetAdminByEmail(ctx context.Context, email string) ([]ResolvedAdmin, error) {
	rows, err := d.pool.Query(ctx, getAdminByEmailSQL, email)
	if err != nil {
		return nil, fmt.Errorf("db: resolve admin by email: %w", err)
	}
	defer rows.Close()

	// Non-nil empty slice: "no admin" is a normal answer here, and a caller must not
	// have to distinguish nil from empty to get it right.
	admins := []ResolvedAdmin{}
	for rows.Next() {
		// The digest is scanned into a local and wrapped, rather than scanned
		// straight into the field: the redacting type's storage is a pointer to an
		// unexported field, so it is not a scan target, and going through
		// NewPasswordHash keeps ONE constructor for the wrapped form. The
		// constructor copies its argument, so the per-iteration reuse of `digest`
		// does not alias every row onto one string.
		var (
			a      ResolvedAdmin
			digest string
		)
		if err := rows.Scan(&a.ID, &a.TenantID, &digest, &a.Status); err != nil {
			return nil, fmt.Errorf("db: resolve admin by email: scan: %w", err)
		}
		a.PasswordHash = NewPasswordHash(digest)
		admins = append(admins, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: resolve admin by email: %w", err)
	}
	return admins, nil
}

// GetAdminSessionByTokenHash resolves a PANEL cookie's token hash to its session
// (id, tenant, admin, revocation) WITHOUT a tenant context -- every panel request
// arrives with only a cookie, so the tenant is the RESULT here too. Returns
// pgx.ErrNoRows when the hash is unknown (the caller redirects to the login page
// and writes nothing).
//
// It answers "whose session is this and is it revoked", NOT "may this request
// proceed". The caller establishes the tenant context from the result and then
// calls store.TouchAdminSession, which is the authority: it refuses a revoked
// session AND a session whose admin is no longer active, in one statement, under
// RLS.
//
// An EMPLOYEE session token cannot resolve here: it is a row in a different table
// reached by a different function (M1-11). That separation is why an employee
// cookie can never reach the panel.
func (d *DB) GetAdminSessionByTokenHash(ctx context.Context, tokenHash string) (ResolvedAdminSession, error) {
	var s ResolvedAdminSession
	err := d.pool.QueryRow(ctx, getAdminSessionByTokenHashSQL, tokenHash).Scan(
		&s.ID, &s.TenantID, &s.AdminUserID, &s.RevokedAt,
	)
	if err != nil {
		// Neither the token nor its hash is named: the argument is not interpolated
		// and %w wraps only the DB error (section 4.7).
		return ResolvedAdminSession{}, fmt.Errorf("db: resolve admin session by hash: %w", err)
	}
	return s, nil
}
