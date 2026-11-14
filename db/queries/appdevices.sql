-- appdevices.sql -- the native app's devices and their single-use request nonces (M16-03, ADR 0045
-- §2.3; migration 00104).
--
-- Every statement here runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md
-- §4.5, belt and braces). The one context-less read of app_devices is the device resolver,
-- resolve_app_device_by_id, hand-written in internal/db/resolve.go (sqlc cannot type a RETURNS
-- TABLE call -- resolve.sql). It is NOT duplicated here: a signed request names a device id and no
-- tenant, the resolver answers the tenant, and only then do these run.
--
-- KIRMIZI CIZGI (§4.7, §7): no query RETURNS public_key, key_digest or nonce_hash -- tappa_app
-- cannot SELECT the first two at all (00104), and the hash exists to be matched. The nonce itself
-- never reaches the database: nonce_hash is computed by internal/appdevice (M16-05).
--
-- 🔴 EVERY WRITE TO revoked_at IS COALESCE(revoked_at, now()) OR CARRIES `revoked_at IS NULL`
-- (00011's BOUNDARY 2; db/queries/sessions.sql's note). The app_devices_revocation_one_way trigger
-- refuses a revoked_at that moves, so a bare `SET revoked_at = now()` would reach it the moment two
-- revocations race (the device's own sign-out against the panel's) and roll the winner's caller
-- back. With the guard the loser re-reads the committed row and writes nothing new.
--
-- DEACTIVATION MUST NOT CALL THE REVOCATIONS (the user's decision of 2026-08-07, sessions.sql's
-- RevokeSessionsForEmployee note): a deactivated person's device stays live so that their next tap
-- reaches §5 row 4 and is RECORDED; a revoked device would push it onto row 3's recordless path.

-- name: CreateAppDevice :one
-- Births an ACTIVE device at the end of activation (M16-07, in the transaction that consumes the
-- invite). tenant_id is explicit and the RLS WITH CHECK confirms it equals the context; the
-- composite key to employees confirms the person is in the same tenant. id, key_digest
-- (GENERATED from public_key) and created_at are the database's. A key already registered -- live
-- or revoked, in any tenant -- is refused by app_devices_key_digest_key (23505).
INSERT INTO app_devices (tenant_id, employee_id, platform, public_key, device_label)
VALUES (@tenant_id, @employee_id, @platform, @public_key, sqlc.narg('device_label'))
RETURNING id, tenant_id, employee_id, platform, device_label, created_at, last_used_at, revoked_at;

-- name: TouchAppDevice :one
-- Refreshes last_used_at on use. revoked_at IS NULL makes it fail CLOSED: a device revoked between
-- resolution and this write updates 0 rows (pgx.ErrNoRows) instead of being stamped as fresh.
UPDATE app_devices
SET last_used_at = now()
WHERE id = @id
  AND tenant_id = @tenant_id
  AND revoked_at IS NULL
RETURNING id, last_used_at;

-- name: RevokeAppDevice :one
-- Revokes one device (its own sign-out, POST /app/v1/revoke; the panel's single-device revoke,
-- M16-26). COALESCE keeps the FIRST stamp, so a repeat is idempotent and never rewrites when the
-- device died. The row is matched whatever its state, so pgx.ErrNoRows means "no such device in
-- this tenant" -- a different answer from "already revoked".
UPDATE app_devices
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = @id
  AND tenant_id = @tenant_id
RETURNING id, revoked_at;

-- name: RevokeAppDevicesForEmployee :many
-- Revokes EVERY live device of one person and returns the ids it revoked (empty when none was live
-- -- idempotent): the panel's "Sign out their phones" (T59) reaching the app (ADR 0045 §2.3). The
-- caller refuses it for a deactivated person (tenant.ErrSignOutDeactivated) -- see the header.
UPDATE app_devices
SET revoked_at = now()
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND revoked_at IS NULL
RETURNING id;

-- name: IssueAppRequestNonce :one
-- Records a nonce for a LIVE device and returns when it expires. The INSERT reads the device row in
-- the same statement: a revoked device, a device of another tenant or an unknown id inserts nothing
-- and the caller sees pgx.ErrNoRows -- "a device that is not active is given no nonce" (ADR 0045
-- §2.3) is the statement's shape, not a check before it. ttl_seconds is the caller's; the table's
-- CHECK caps it at 5 minutes (23514 above that). created_at is the database's now().
INSERT INTO app_request_nonces (tenant_id, device_id, nonce_hash, expires_at)
SELECT d.tenant_id, d.id, @nonce_hash::text, now() + make_interval(secs => @ttl_seconds::int)
FROM app_devices AS d
WHERE d.id = @device_id
  AND d.tenant_id = @tenant_id
  AND d.revoked_at IS NULL
RETURNING id, expires_at;

-- name: ConsumeAppRequestNonce :one
-- 🔴 THE SINGLE-USE CHECK, IN ONE STATEMENT (§4.4's shape -- the tags.last_ctr discipline). Matches
-- the nonce only when it is THIS device's, in THIS tenant, unconsumed and unexpired, and stamps it
-- in the same statement. pgx.ErrNoRows -> reject: unknown, another device's, replayed or expired --
-- the caller does not need to tell them apart, and must never read the row first and write later.
-- Two racing consumptions of one nonce: the second waits on the first's row lock and, after its
-- commit, re-checks consumed_at IS NULL against the committed row -- exactly one RETURNING row.
UPDATE app_request_nonces
SET consumed_at = now()
WHERE tenant_id = @tenant_id
  AND device_id = @device_id
  AND nonce_hash = @nonce_hash
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING id;

-- name: CountLiveAppRequestNonces :one
-- How many unconsumed, unexpired nonces one device holds: M16-07's cap on live nonces per device
-- (the table cannot be inflated by a device asking without spending).
SELECT count(*)::int AS live
FROM app_request_nonces
WHERE tenant_id = @tenant_id
  AND device_id = @device_id
  AND consumed_at IS NULL
  AND expires_at > now();

-- name: DeleteDeadAppRequestNonces :execrows
-- The cleanup decision of 00104: a device's dead nonces (consumed, or expired) are deleted by the
-- issuing path before it issues the next one. Fail-closed by construction -- a consumption needs a
-- live row, so a deleted row can only ever be a reject. Never touches a live nonce.
DELETE FROM app_request_nonces
WHERE tenant_id = @tenant_id
  AND device_id = @device_id
  AND (consumed_at IS NOT NULL OR expires_at <= now());
