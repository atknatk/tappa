-- 00052 -- M11 API-4: api_idempotency, the integration API's Idempotency-Key ledger.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule: REVOKE ALL
-- from tappa_app, then the verbs and the columns it needs; UPDATE always by column) and §8 (the
-- table, UNIQUE(tenant_id, key_id, idem_key), the row written IN THE SAME TRANSACTION as the write
-- it guards, no response body stored -- the status and the record's id only --, a different body
-- under the same key 422, rows older than 24 hours deleted lazily on the request path, for that
-- tenant alone -- K-19, no cross-tenant sweep).
--
-- HOW A ROW IS USED (internal/apikey, Writes.Run -- every statement in WithTenant(the key's tenant)
-- with an explicit tenant_id filter):
--   1. the claim: INSERT ... ON CONFLICT (tenant_id, key_id, idem_key) DO NOTHING, BEFORE the work.
--      A second request with the same key waits on this table's primary key until the first one's
--      transaction ends (that wait IS the concurrency control -- no advisory lock, no "processing"
--      row in a transaction of its own: a process that dies mid-request rolls its claim back and
--      leaves nothing stuck);
--   2. the work, in the same transaction;
--   3. the result: UPDATE ... SET status, result_ref WHERE status IS NULL -- still in it.
--   A request that finds a committed row replays it: the same request_hash gives back the status and
--   the record's id, a different one is 422. status is NULL only inside the claiming transaction.
--
-- request_hash is the SHA-256 of the method, the route and the body (internal/handler/api). It is
-- not a secret and carries nothing the tenant did not send; it is kept 24 hours.

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.api_keys') IS NULL THEN
        RAISE EXCEPTION 'migration 00052 (M11 API-4) needs migration 00051 (api_keys). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.api_idempotency') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00052 (M11 API-4) creates api_idempotency, and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE api_idempotency (
    tenant_id    uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    key_id       uuid        NOT NULL,
    idem_key     text        NOT NULL,
    request_hash text        NOT NULL,
    status       smallint,
    result_ref   uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- UNIQUE(tenant_id, key_id, idem_key) of §8, as the primary key: one key's namespace, inside its
    -- tenant. Its leading column is tenant_id -- the RLS five's index.
    CONSTRAINT api_idempotency_pkey PRIMARY KEY (tenant_id, key_id, idem_key),
    -- The key is the tenant's own (the composite key api_keys_id_tenant_key).
    CONSTRAINT api_idempotency_key_fk FOREIGN KEY (key_id, tenant_id)
        REFERENCES api_keys (id, tenant_id) ON DELETE RESTRICT,
    -- The header's shape (internal/handler/api refuses anything else with 400 before it gets here):
    -- 1 to 255 visible ASCII characters.
    CONSTRAINT api_idempotency_key_shape CHECK (idem_key ~ '^[!-~]{1,255}$'),
    CONSTRAINT api_idempotency_hash_shape CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    -- Only a write that committed is recorded, and a committed write is a 2xx; an error rolls the
    -- claim back with the work.
    CONSTRAINT api_idempotency_status_range CHECK (status IS NULL OR status BETWEEN 200 AND 299)
);

-- The lazy delete's walk: a tenant's oldest rows first.
CREATE INDEX api_idempotency_tenant_created_idx ON api_idempotency (tenant_id, created_at);

ALTER TABLE api_idempotency ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_idempotency FORCE ROW LEVEL SECURITY;
CREATE POLICY api_idempotency_tenant_isolation ON api_idempotency
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- tappa_app (ADR 0028 §1's matrix row "api_idempotency -- API-4"): reads the rows; claims one by
-- naming its tenant, key, idempotency key and request hash (created_at is the default, status and
-- result_ref start NULL); writes the result into status and result_ref alone; deletes the expired
-- rows (the lazy delete is a DELETE). No table-level UPDATE: it would override the column list.
REVOKE ALL ON api_idempotency FROM tappa_app;
GRANT SELECT (tenant_id, key_id, idem_key, request_hash, status, result_ref, created_at) ON api_idempotency TO tappa_app;
GRANT INSERT (tenant_id, key_id, idem_key, request_hash) ON api_idempotency TO tappa_app;
GRANT UPDATE (status, result_ref) ON api_idempotency TO tappa_app;
GRANT DELETE ON api_idempotency TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_table_privilege('tappa_app', 'public.api_idempotency', 'TRUNCATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.api_idempotency', 'REFERENCES')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.api_idempotency', 'TRIGGER')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.api_idempotency'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('status', 'result_ref')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE')) THEN
        RAISE EXCEPTION 'migration 00052 (M11 API-4): tappa_app must update status and result_ref alone and must not truncate, reference or trigger api_idempotency -- through PUBLIC or a role membership it holds. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- The ledger goes with the table: a request retried after this answers as a first request would.
-- It names no operator object (opAtVersion skips it); its DROP TABLE takes tenants and api_keys (the
-- table's two keys), which holdingDownTables lists.
DROP TABLE IF EXISTS api_idempotency;
