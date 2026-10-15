-- apikeys.sql -- the integration API's keys (M11 API-3, ADR 0028 §3; migration 00051).
--
-- Every statement here runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md
-- §4.5, belt and braces). The one context-less read of this table is the seventh resolver,
-- resolve_api_key_by_hash, which is hand-written in internal/db/resolve.go (sqlc cannot type a
-- RETURNS TABLE call -- resolve.sql).
--
-- 🔴 EVERY WRITE TO revoked_at CARRIES `revoked_at IS NULL` AND COALESCE (00011's BOUNDARY 2). The
-- api_keys_revocation_one_way trigger refuses a revoked_at that moves; a revocation written as a
-- bare `SET revoked_at = now()` would reach it the moment two revocations race -- the owner's
-- double click, or "revoke one" against "revoke all" -- and the trigger's exception would roll the
-- WINNING transaction's caller back: the panel reports failure while the key lives. With the
-- guard, the loser re-reads the committed row under its lock, matches nothing and returns no row,
-- so it writes no audit row either (TestApiKeyRevocation_ConcurrentAndBulkAreSafe).

-- name: InsertAPIKey :one
-- id and created_at are the table's defaults; tappa_app cannot write them (00051).
INSERT INTO api_keys (tenant_id, name, token_hash, prefix_hint, created_by)
VALUES (@tenant_id, @name, @token_hash, @prefix_hint, @created_by)
RETURNING id, created_at;

-- name: ListAPIKeys :many
-- The panel's list: newest first, revoked ones included (the owner sees what was issued and
-- when it stopped working). token_hash is not readable by tappa_app and is not named.
SELECT id, name, prefix_hint, created_by, created_at, last_used_at, revoked_at
FROM api_keys
WHERE tenant_id = @tenant_id
ORDER BY created_at DESC, id;

-- name: RevokeAPIKey :one
-- One key, by its id in this tenant. No row: the key is unknown here OR was revoked already
-- (by another request that won the race, or earlier); the caller does not write an audit row.
UPDATE api_keys
SET revoked_at = COALESCE(revoked_at, now())
WHERE tenant_id = @tenant_id
  AND id = @id
  AND revoked_at IS NULL
RETURNING id, name, prefix_hint, revoked_at;

-- name: RevokeAllAPIKeys :many
-- Every live key of this tenant -- the incident button. Only the rows THIS statement revoked come
-- back; a key another request revoked first is not among them.
UPDATE api_keys
SET revoked_at = COALESCE(revoked_at, now())
WHERE tenant_id = @tenant_id
  AND revoked_at IS NULL
RETURNING id, name, prefix_hint, revoked_at;

-- name: TouchAPIKey :execrows
-- last_used_at at most once a minute per key (ADR 0028 §3): a busy integration writes this row
-- once a minute rather than once a request. A revoked key is not touched.
UPDATE api_keys
SET last_used_at = now()
WHERE tenant_id = @tenant_id
  AND id = @id
  AND revoked_at IS NULL
  AND (last_used_at IS NULL OR last_used_at <= now() - interval '1 minute');

-- name: GetTenantAPIEnabledAt :one
-- K-22's gate as the panel reads it: NULL is closed. The owner may revoke a key whatever this says
-- (ADR 0028 §10, K-26); a new key needs it open.
SELECT api_enabled_at
FROM tenants
WHERE id = @tenant_id;

-- name: GetAPIKeyRevokedAt :one
-- After a revocation matched no row: is the key unknown in this tenant (no row) or revoked
-- already (a timestamp)? Read in the same transaction, for the panel's sentence only.
SELECT revoked_at
FROM api_keys
WHERE tenant_id = @tenant_id
  AND id = @id;
