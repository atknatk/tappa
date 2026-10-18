-- apiidempotency.sql -- the integration API's Idempotency-Key ledger (M11 API-4, ADR 0028 §8;
-- migration 00052). internal/apikey's Writes.Run is the one caller.
--
-- Every statement runs inside WithTenant(the key's tenant) and carries an explicit tenant_id filter
-- (CLAUDE.md §4.5, belt and braces). None of them reaches another tenant's rows: there is no
-- cross-tenant sweep (K-19) -- the 24-hour expiry is applied lazily, on the request path, to the
-- requesting tenant's rows alone.

-- name: DeleteExpiredIdempotencyForKey :execrows
-- The one row this request's key would collide with, when it is older than 24 hours: after it the
-- key is free again. It may wait for a concurrent request that is deleting the same row -- a request
-- with the same key is meant to wait.
DELETE FROM api_idempotency
WHERE tenant_id = @tenant_id
  AND key_id = @key_id
  AND idem_key = @idem_key
  AND created_at < now() - interval '24 hours';

-- name: DeleteExpiredIdempotency :execrows
-- The lazy delete: at most 100 of this tenant's rows older than 24 hours, oldest first, SKIP LOCKED.
-- It runs in a short transaction of its own (internal/apikey, Writes.Run): inside a write's
-- transaction its locks lived as long as the write and two writes deadlocked on each other's swept
-- rows (measured, API-4 review D1).
-- 🔴 THE BATCH IS A MATERIALIZED CTE (T130 fix round 1), NOT `(...) IN (SELECT ... LIMIT 100 FOR
-- UPDATE SKIP LOCKED)`: that shape deleted 105 of 105 under a nested loop semi join -- every rescan
-- of the subquery skips the rows this statement already locked and its LIMIT takes the next ones (the
-- trap webhooks.sql's sweep measured first). Every row it reached was still an expired row of this
-- tenant; the loss was the bound. Materialized, the candidate list is computed once
-- (internal/db TestApiIdempotency_TheLazyDeleteIsBounded, under a forced nested loop too).
WITH doomed AS MATERIALIZED (
    SELECT e.key_id, e.idem_key
    FROM api_idempotency AS e
    WHERE e.tenant_id = @tenant_id
      AND e.created_at < now() - interval '24 hours'
    ORDER BY e.created_at
    LIMIT 100
    FOR UPDATE SKIP LOCKED)
DELETE FROM api_idempotency AS d
USING doomed
WHERE d.tenant_id = @tenant_id
  AND d.key_id = doomed.key_id
  AND d.idem_key = doomed.idem_key;

-- name: ClaimIdempotency :execrows
-- The claim, before the work. 1: this request is the first with the key. 0: a row exists -- a
-- committed one, or one a concurrent transaction committed while this statement waited on the
-- primary key (the wait is the concurrency control, ADR 0028 §8).
INSERT INTO api_idempotency (tenant_id, key_id, idem_key, request_hash)
VALUES (@tenant_id, @key_id, @idem_key, @request_hash)
ON CONFLICT (tenant_id, key_id, idem_key) DO NOTHING;

-- name: GetIdempotency :one
-- The recorded request after a claim found a row.
SELECT request_hash, status, result_ref
FROM api_idempotency
WHERE tenant_id = @tenant_id
  AND key_id = @key_id
  AND idem_key = @idem_key;

-- name: RecordIdempotency :execrows
-- The result, in the claiming transaction: the status and the record's id, never a body. Only the
-- claim's own row (status IS NULL) is written.
UPDATE api_idempotency
SET status = @status, result_ref = @result_ref
WHERE tenant_id = @tenant_id
  AND key_id = @key_id
  AND idem_key = @idem_key
  AND status IS NULL;
