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
--
-- 🔴 THE REVOCATION KILLS THE DEVICE'S NONCES IN THE SAME STATEMENT (M16-03 security review): a nonce
-- issued before the revoke would otherwise stay spendable for its lifetime. The DELETE runs in this
-- statement whatever the SELECT reads (a data-modifying CTE always runs once). A consumption that
-- already holds a nonce's row lock makes this DELETE wait for it, so that consumption commits BEFORE
-- the revocation; one that comes later finds the row gone.
--
-- ⚠️ THE DELETE DOES NOT CATCH EVERY NONCE, SO ConsumeAppRequestNonce's BELT IS LOAD-BEARING. In the
-- order "issue in flight, then revoke" the UPDATE waits for the issue's FOR SHARE lock, but this
-- statement's snapshot predates the issue's commit, so the DELETE does not see that nonce and it
-- survives the revocation (measured by the M16-03 security re-check). Only the EXISTS ... FOR SHARE
-- in ConsumeAppRequestNonce refuses it. Neither half may be removed as redundant.
WITH r AS (
    UPDATE app_devices AS d
    SET revoked_at = COALESCE(d.revoked_at, now())
    WHERE d.id = @id
      AND d.tenant_id = @tenant_id
    RETURNING d.id, d.revoked_at
), killed AS (
    DELETE FROM app_request_nonces AS n
    USING r
    WHERE n.device_id = r.id
      AND n.tenant_id = @tenant_id
    RETURNING n.id
)
SELECT r.id, r.revoked_at FROM r;

-- name: RevokeAppDevicesForEmployee :many
-- Revokes EVERY live device of one person and returns the ids it revoked (empty when none was live
-- -- idempotent): the panel's "Sign out their phones" (T59) reaching the app (ADR 0045 §2.3). The
-- caller refuses it for a deactivated person (tenant.ErrSignOutDeactivated) -- see the header.
-- Like RevokeAppDevice it deletes the revoked devices' nonces in the same statement.
WITH r AS (
    UPDATE app_devices AS d
    SET revoked_at = now()
    WHERE d.tenant_id = @tenant_id
      AND d.employee_id = @employee_id
      AND d.revoked_at IS NULL
    RETURNING d.id
), killed AS (
    DELETE FROM app_request_nonces AS n
    USING r
    WHERE n.device_id = r.id
      AND n.tenant_id = @tenant_id
    RETURNING n.id
)
SELECT r.id FROM r;

-- name: IssueAppRequestNonce :one
-- Records a nonce for a LIVE device and returns when it expires. The INSERT reads the device row in
-- the same statement: a revoked device, a device of another tenant or an unknown id inserts nothing
-- and the caller sees pgx.ErrNoRows -- "a device that is not active is given no nonce" (ADR 0045
-- §2.3) is the statement's shape, not a check before it. ttl_seconds is the caller's; the table's
-- CHECK caps it at 5 minutes (23514 above that). created_at is the database's now().
--
-- FOR SHARE OF d: an issue racing a revocation waits for the revoking transaction and then re-reads
-- the committed device row (READ COMMITTED re-checks a locked row's qual on its newest version), so
-- it inserts nothing -- instead of inserting a live nonce AFTER the revocation deleted the others.
-- FOR SHARE needs UPDATE on some column of app_devices, which tappa_app holds (last_used_at,
-- revoked_at); it does not conflict with another FOR SHARE, so two issues do not wait for each other.
INSERT INTO app_request_nonces (tenant_id, device_id, nonce_hash, expires_at)
SELECT d.tenant_id, d.id, @nonce_hash::text, now() + make_interval(secs => @ttl_seconds::int)
FROM app_devices AS d
WHERE d.id = @device_id
  AND d.tenant_id = @tenant_id
  AND d.revoked_at IS NULL
FOR SHARE OF d
RETURNING id, expires_at;

-- name: ConsumeAppRequestNonce :one
-- 🔴 THE SINGLE-USE CHECK, IN ONE STATEMENT (§4.4's shape -- the tags.last_ctr discipline). Matches
-- the nonce only when it is THIS device's, in THIS tenant, unconsumed and unexpired, and stamps it
-- in the same statement. pgx.ErrNoRows -> reject: unknown, another device's, replayed or expired --
-- the caller does not need to tell them apart, and must never read the row first and write later.
-- Two racing consumptions of one nonce: the second waits on the first's row lock and, after its
-- commit, re-checks consumed_at IS NULL against the committed row -- exactly one RETURNING row.
--
-- THE BELT (M16-03 security review) -- LOAD-BEARING, NOT REDUNDANT: the device must be unrevoked IN
-- THIS STATEMENT, not only in the resolver's earlier answer. The revocations delete the nonces
-- (RevokeAppDevice), which is what usually refuses here; the EXISTS refuses a nonce that survived a
-- revocation written any other way AND the nonce an in-flight issue commits behind the revoke's
-- snapshot (RevokeAppDevice's note -- the delete cannot see it, only this refuses it). FOR SHARE
-- makes it hold under a race, not only after one: a consumption meeting an uncommitted revocation
-- waits for it and re-checks revoked_at on the committed row (without the lock the subquery would
-- read its snapshot and pass). Within this one statement the order is the revocation's -- the device
-- row, then the nonce row -- and two consumptions of one device take compatible share locks.
-- (TestAppRequestNonces_RevocationAndConsumptionRace measures these.)
--
-- ⚠️ A CALLER COMPOSING STATEMENTS CAN STILL DEADLOCK (measured by the M16-03 security re-check):
-- a request that consumes and THEN runs TouchAppDevice upgrades its share lock on the device row to
-- an update lock, and two such requests deadlock (40P01); DeleteDeadAppRequestNonces and then
-- IssueAppRequestNonce in one transaction take a nonce row before the device row, and a concurrent
-- revoke deadlocks with it -- the measured victim was the REVOKE, so a stolen phone could keep a
-- revocation failing. The rules, for M16-05 and M16-07: the device row first, then nonce rows;
-- Touch BEFORE consume; the dead-nonce sweep AFTER issuing or in its own transaction; the revoke's
-- caller retries on 40P01.
UPDATE app_request_nonces AS n
SET consumed_at = now()
WHERE n.tenant_id = @tenant_id
  AND n.device_id = @device_id
  AND n.nonce_hash = @nonce_hash
  AND n.consumed_at IS NULL
  AND n.expires_at > now()
  AND EXISTS (
      SELECT 1 FROM app_devices AS d
      WHERE d.id = n.device_id
        AND d.tenant_id = n.tenant_id
        AND d.revoked_at IS NULL
      FOR SHARE
  )
RETURNING n.id;

-- name: CountLiveAppRequestNonces :one
-- How many unconsumed, unexpired nonces one device holds: a READ for M16-07's cap on live nonces per
-- device (the table cannot be inflated by a device asking without spending). ⚠️ It is NOT the cap:
-- this count and then IssueAppRequestNonce is read-then-write, and concurrent issues exceed it.
-- Folding the count into the INSERT's WHERE does NOT fix it either: measured by the M16-03 security
-- re-check, with FOR SHARE OF d and with FOR UPDATE OF d two concurrent issues at a cap of 1 both
-- landed. The cap needs the device row locked in a PREVIOUS statement of the same transaction
-- (SELECT ... FOR NO KEY UPDATE) or a per-device advisory lock, before the count (M16-07's card).
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
