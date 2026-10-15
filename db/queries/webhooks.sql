-- webhooks.sql -- the owner's webhook endpoint (M11 API-9, ADR 0028 §6; table from migration 00048).
--
-- Every statement runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md §4.5,
-- belt and braces). tappa_app holds SELECT, INSERT (tenant_id, id, url, signing_key_sealed, active)
-- and UPDATE (url, signing_key_sealed, active, rotated_at) on the table -- never DELETE, never
-- tenant_id (00048, ADR 0028 §1's matrix).
--
-- 🔴 THE SEALED KEY IS READ BY ONE STATEMENT: GetWebhookEndpointForSigning, for the ping (and, with
-- API-10, the delivery). The panel's read names no key column. The url is never logged by any
-- caller (CLAUDE.md §7).

-- name: GetWebhookEndpoint :one
-- The panel's read: the business's one endpoint (UNIQUE (tenant_id)), without its key.
SELECT id, url, active, created_at, rotated_at
FROM webhook_endpoints
WHERE tenant_id = @tenant_id;

-- name: GetWebhookEndpointForSigning :one
-- The endpoint with its sealed key, for a request that signs (the ping). The key is opened with the
-- row's own id as AAD, so a sealed value moved to another row does not open.
SELECT id, url, signing_key_sealed, active
FROM webhook_endpoints
WHERE tenant_id = @tenant_id;

-- name: InsertWebhookEndpoint :one
-- The id is chosen by the caller BEFORE the insert: it is the sealed key's AAD. created_at and
-- rotated_at are the table's (default, NULL). A second endpoint of the business meets
-- webhook_endpoints_one_per_tenant (23505).
INSERT INTO webhook_endpoints (tenant_id, id, url, signing_key_sealed, active)
VALUES (@tenant_id, @id, @url, @signing_key_sealed, true)
RETURNING created_at;

-- name: RotateWebhookEndpointKey :one
-- A new sealed key for THIS endpoint (the id is the seal's AAD -- a key sealed for another id would
-- not open here), and the moment it changed. The old key no longer signs anything from the commit on.
UPDATE webhook_endpoints
SET signing_key_sealed = @signing_key_sealed,
    rotated_at = clock_timestamp()
WHERE tenant_id = @tenant_id
  AND id = @id
RETURNING rotated_at;

-- name: ReaddressWebhookEndpoint :execrows
-- A new address for THIS endpoint; its key is unchanged.
UPDATE webhook_endpoints
SET url = @url
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: SetWebhookEndpointActive :execrows
-- Switch the endpoint off or on. Only a change matches: switching off an endpoint that is off writes
-- nothing, so the caller writes no second trail row.
UPDATE webhook_endpoints
SET active = @active
WHERE tenant_id = @tenant_id
  AND id = @id
  AND active <> @active;

-- name: LockWebhookBucket :one
-- SERIALISE ONE BUSINESS'S PINGS (and, with API-10, its "resend"s -- ADR 0028 §6: one bucket, ten an
-- hour). The bucket is a READ-THEN-DECIDE over audit_log (ReadWebhookBucket), and without a lock N
-- concurrent pings all count before any has committed and all pass. LockAPIDeactivationBudget's
-- shape and argument, verbatim: the lock is taken in the SELECT LIST of this business's tenants row
-- (RLS and the explicit predicate), so a context in which the row is not visible returns NO ROW and
-- takes no lock, which the caller treats as a failure. The namespace keeps it apart from every other
-- advisory key.
SELECT t.id,
       (pg_advisory_xact_lock(hashtextextended('webhook-bucket:' || (sqlc.arg(tenant_id)::uuid)::text, 0)) IS NULL)::boolean AS unlocked
FROM tenants t
WHERE t.id = @tenant_id;

-- name: ReadWebhookBucket :one
-- How many slots of the bucket this business spent in the last hour, and when the oldest was spent
-- (the next slot frees an hour after it). A slot is a trail row of one of @actions -- the ping's row
-- is written BEFORE the request goes out, in the transaction that holds the lock, so a slot is spent
-- whether or not the remote answers and a crash after the commit cannot give it back. `at` is the
-- database's clock (00053: tappa_app cannot write it). audit_log_tenant_at_idx serves the range.
SELECT count(*)::int AS spent,
       coalesce(min(at), now())::timestamptz AS oldest
FROM audit_log
WHERE tenant_id = @tenant_id
  AND action = ANY (@actions::text[])
  AND at > now() - interval '1 hour';
