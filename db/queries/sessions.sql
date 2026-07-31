-- sessions.sql -- the TENANT-SCOPED half of "proof of person" (CLAUDE.md
-- section 5): issuing, refreshing, revoking and listing the persistent browser
-- session that answers "who is this phone?".
--
-- Every query below carries an EXPLICIT tenant_id predicate (CLAUDE.md section
-- 4.5, belt + braces on top of RLS) and is meant to run inside
-- db.(*DB).WithTenant, i.e. with app.tenant_id established. There is exactly ONE
-- session lookup in the codebase that does NOT carry a tenant filter -- the
-- context-less resolution path GetEmployeeBySessionHash (db/queries/resolve.sql,
-- ADR 0002 madde 7) -- because there the tenant is the RESULT of the lookup, not
-- an input. It is deliberately NOT duplicated here.
--
-- KIRMIZI CIZGI (section 4.7): the raw session token NEVER appears in this file
-- or in the database. Only token_hash (HMAC-SHA256 of the token under
-- TAPPA_SESSION_HMAC_KEY) is ever written or matched, and no query below RETURNS
-- token_hash: the column exists to be matched, not to be handed back to Go.
-- device_info is a COARSE device label only ("iPhone Safari"), nullable, and is
-- never derived from browser probing (section 4.1).
--
-- NO DELETE QUERIES BY DESIGN: migration 00003 REVOKEs DELETE on sessions from
-- tappa_app, so a session ROW cannot be destroyed (section 4.6 spirit).
-- Revocation is a revoked_at stamp instead, and a revoked row stays readable and
-- still resolves to its employee -- that is what lets the tap path RECORD a
-- revoked-session attempt instead of losing it.
--
-- THE LIMIT OF THAT GUARANTEE, stated precisely. "The trail survives" is true for
-- DELETE and only for DELETE. tappa_app holds table-wide UPDATE on sessions --
-- it must, because TouchSession writes last_used_at -- and Postgres has no
-- column-level way here to allow that while forbidding an UPDATE that sets
-- revoked_at back to NULL. So the protection against un-revoking is that NO
-- QUERY IN THIS FILE DOES IT: RevokeSession only ever COALESCEs to a non-NULL
-- value, RevokeSessionsForEmployee only sets now(), TouchSession touches
-- last_used_at alone and additionally requires revoked_at IS NULL. That is a
-- property of this file, not an enforcement by the database. Anyone adding a
-- query here must keep it: revoked_at goes NULL -> non-NULL, never back.
-- Enforcing it structurally would need a trigger in a new migration (00003 is
-- applied and immutable, CLAUDE.md section 6).

-- name: CreateSession :one
-- Issues a session row for an already-activated employee. tenant_id is supplied
-- explicitly and the RLS WITH CHECK confirms it equals the transaction context,
-- so a mismatched tenant is refused by the database, not merely by the caller.
-- token_hash is the HMAC of a token this query never sees; it is written, never
-- returned. device_info is NULL when the caller has no coarse label.
INSERT INTO sessions (tenant_id, employee_id, token_hash, device_info)
VALUES (@tenant_id, @employee_id, @token_hash, sqlc.narg('device_info'))
RETURNING id, tenant_id, employee_id, device_info, created_at, last_used_at, revoked_at;

-- name: TouchSession :one
-- Refreshes last_used_at on use (the card's "kullanimda yenilenir"). The
-- revoked_at IS NULL guard makes the refresh fail CLOSED: a session revoked
-- between verification and this write updates 0 rows and the caller sees
-- pgx.ErrNoRows instead of silently stamping a dead session as freshly used.
-- Returns nothing but the row identity and the new stamp -- no hash, no token.
UPDATE sessions
SET last_used_at = now()
WHERE id = @id
  AND tenant_id = @tenant_id
  AND revoked_at IS NULL
RETURNING id, last_used_at;

-- name: RevokeSession :one
-- Revokes one session. COALESCE keeps the FIRST revocation timestamp: a repeated
-- revoke is idempotent AND does not rewrite when the session actually died
-- (audit truth). Because the row is matched regardless of its revocation state,
-- pgx.ErrNoRows means "no such session in this tenant" -- a genuinely different
-- outcome from "already revoked", which callers must be able to tell apart.
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = @id
  AND tenant_id = @tenant_id
RETURNING id, revoked_at;

-- name: RevokeSessionsForEmployee :many
-- Revokes EVERY live session of one employee and returns the ids it revoked
-- (empty when there was nothing live -- idempotent). It is deliberately the ONLY
-- "instant invalidation" mechanism this layer owns.
--
-- LEGITIMATE CALLERS: a lost or stolen phone, and the second-activation decision
-- (M5-02) when a new device replaces an old one -- cases where the SESSION is
-- what went wrong.
--
-- DEACTIVATION MUST NOT CALL THIS (this comment previously said the opposite).
-- Rejecting a deactivated employee does not need revocation: tap.Decide reads
-- employees.status into the policy context and sys:employee-deactivated denies
-- with a security alert on that field alone. Revoking would add nothing to the
-- reject and would push every later tap by that person onto the "revoked" branch,
-- where a caller taking the obvious shortcut writes NO record -- breaking
-- CLAUDE.md section 4.6. The session layer carries truth and never decides:
-- reject-with-alert (section 5 row 4) versus activation redirect (row 3) is the
-- guardrail's call, not this query's.
UPDATE sessions
SET revoked_at = now()
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND revoked_at IS NULL
RETURNING id;

-- name: ListSessionsForEmployee :many
-- All sessions of one employee, newest first: the read side of the optional
-- device limit (M5-01 card: "config ile kapali gelebilir" -- NOT enforced here)
-- and of the M5-02 "is this a new phone or an attack?" decision. Revoked rows are
-- included on purpose; the caller filters on revoked_at, so history stays visible.
-- token_hash is NOT selected: nothing outside the resolution path needs it.
SELECT id, tenant_id, employee_id, device_info, created_at, last_used_at, revoked_at
FROM sessions
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
ORDER BY created_at DESC;
