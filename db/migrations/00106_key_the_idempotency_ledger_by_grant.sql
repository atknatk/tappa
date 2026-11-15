-- 00106 -- issue #37 phase 1: api_idempotency learns the second principal -- an OAuth grant
-- beside an API key.
--
-- NORMATIVE SOURCE: docs/adr/0047-oauth-bagli-uygulamalar.md §8.7 ("Idempotency") and §14.5:
-- grant_id uuid NULL (-> oauth_grants (id, tenant_id)), key_id NULL-able, exactly one of the two
-- (num_nonnulls = 1), the primary key replaced by two partial UNIQUEs -- one namespace per key and
-- one per grant, each inside its tenant; the Down deletes the grant rows and gives the primary key
-- back. And the ADR's "Uygulama notları": the claim's ON CONFLICT now names the partial index's
-- predicate (WHERE key_id IS NOT NULL) or the inference finds no index; the lazy per-key delete has
-- a grant twin (db/queries/apiidempotency.sql).
--
-- WHY THE KEY'S FOREIGN KEY STAYS: api_idempotency_key_fk (key_id, tenant_id) -> api_keys is MATCH
-- SIMPLE, so a grant row (key_id NULL) passes it untouched, and a key row is still bound to its own
-- tenant's key. The grant's key is the same shape: (grant_id, tenant_id) -> oauth_grants (id,
-- tenant_id) -- a row can never name another tenant's grant.
--
-- WHAT THE API'S CALLERS SEE: nothing changes for a key. Its three statements keep their meaning;
-- the claim's conflict target is now the partial index, which holds exactly the rows the primary key
-- held (every row before this file has a key_id). Two grants -- or a grant and a key -- using the
-- same Idempotency-Key never meet: they are different namespaces.
--
-- LOCKS: the ALTER takes ACCESS EXCLUSIVE on api_idempotency (and SHARE ROW EXCLUSIVE on
-- oauth_grants for the new key); lock_timeout is the first statement (00054's rule) so a long API
-- write in flight makes the file give up with 55P03 instead of queueing every later API write.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.oauth_grants') IS NULL THEN
        RAISE EXCEPTION 'migration 00106 (issue #37) needs migration 00104 (oauth_grants). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF (SELECT pg_catalog.pg_get_constraintdef(c.oid) FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.api_idempotency'::pg_catalog.regclass AND c.conname = 'api_idempotency_pkey')
         IS DISTINCT FROM 'PRIMARY KEY (tenant_id, key_id, idem_key)'
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.api_idempotency'::pg_catalog.regclass AND a.attname = 'grant_id'
                     AND NOT a.attisdropped) THEN
        RAISE EXCEPTION 'migration 00106 (issue #37) replaces api_idempotency''s primary key (tenant_id, key_id, idem_key) of 00052 and adds grant_id, and the table is not in that state. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE api_idempotency
    DROP CONSTRAINT api_idempotency_pkey,
    ALTER COLUMN key_id DROP NOT NULL,
    ADD COLUMN grant_id uuid,
    ADD CONSTRAINT api_idempotency_grant_fk FOREIGN KEY (grant_id, tenant_id)
        REFERENCES oauth_grants (id, tenant_id) ON DELETE RESTRICT,
    -- One principal per row: the key's or the grant's namespace, never both, never neither.
    ADD CONSTRAINT api_idempotency_one_principal CHECK (num_nonnulls(key_id, grant_id) = 1);

-- The two namespaces, each led by tenant_id (the RLS five's index stays api_idempotency_tenant_created_idx).
-- Each is the conflict target of its own claim: ON CONFLICT (tenant_id, key_id, idem_key) WHERE key_id
-- IS NOT NULL, and the grant's twin.
CREATE UNIQUE INDEX api_idempotency_key_key ON api_idempotency (tenant_id, key_id, idem_key) WHERE key_id IS NOT NULL;
CREATE UNIQUE INDEX api_idempotency_grant_key ON api_idempotency (tenant_id, grant_id, idem_key) WHERE grant_id IS NOT NULL;

-- tappa_app reads the new column and names it in a claim; it still updates status and result_ref
-- alone (00052's list is not touched).
GRANT SELECT (grant_id) ON api_idempotency TO tappa_app;
GRANT INSERT (grant_id) ON api_idempotency TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                WHERE a.attrelid = 'public.api_idempotency'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                  AND a.attname NOT IN ('status', 'result_ref')
                  AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR pg_catalog.has_table_privilege('tappa_app', 'public.api_idempotency', 'TRUNCATE') THEN
        RAISE EXCEPTION 'migration 00106 (issue #37): tappa_app must update status and result_ref alone and must not truncate api_idempotency. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- The grants' ledger rows go (ADR 0047 §14.5): a grant request retried after this answers as a first
-- request would -- the same as 00052's Down did for every row. The key rows and their primary key come
-- back unchanged. Its ALTER takes api_idempotency and oauth_grants (the dropped key); it names no
-- operator object (opAtVersion skips it).
SET LOCAL lock_timeout = '1s';
DELETE FROM api_idempotency WHERE grant_id IS NOT NULL;
DROP INDEX IF EXISTS api_idempotency_grant_key;
DROP INDEX IF EXISTS api_idempotency_key_key;
ALTER TABLE api_idempotency
    DROP CONSTRAINT IF EXISTS api_idempotency_one_principal,
    DROP CONSTRAINT IF EXISTS api_idempotency_grant_fk,
    DROP COLUMN IF EXISTS grant_id,
    ALTER COLUMN key_id SET NOT NULL,
    ADD CONSTRAINT api_idempotency_pkey PRIMARY KEY (tenant_id, key_id, idem_key);
