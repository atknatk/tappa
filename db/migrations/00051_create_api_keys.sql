-- 00051 -- M11 API-3: api_keys and the seventh resolver, resolve_api_key_by_hash.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule: REVOKE ALL
-- from tappa_app, then the verbs and the columns it needs; UPDATE always by column; every new
-- function REVOKEd from PUBLIC; a trigger function EXECUTE in its owner alone and asserting
-- TG_RELID), §2 (K-22: the gate is tenants.api_enabled_at), §3 (the table, the global UNIQUE on
-- token_hash, the seventh resolver to ADR 0002 md.7's five constraints, tappa_resolver's new
-- reach: the resolution's columns of api_keys and ONLY SELECT (id, api_enabled_at) ON tenants);
-- docs/adr/0002-tenant-baglami-ve-rls.md md.7 and its M11 note.
--
-- THE TABLE. One row per key the owner issued from the panel. The key itself is never stored:
-- token_hash is the hex HMAC-SHA256 of the whole key under TAPPA_API_KEY_HMAC_KEY
-- (internal/apikey), so a dump of this table alone cannot be tried against the API.
-- prefix_hint is the key's prefix and the first four characters after it -- 24 of the 256 random
-- bits, enough to tell an owner's few keys apart and too few to shrink a guess (ADR 0028's API-3
-- note). created_by is the admin who issued it, in the same tenant (the composite key).
--
-- REVOCATION IS ONE WAY, the 00011 precedent (tappa_forbid_revocation_reset, BOUNDARY 1 and 2):
--   * the trigger refuses revoked_at going back to NULL or to another instant -- its own function,
--     not 00011's, because §1 asks a trigger function born in M11 to keep EXECUTE in its owner
--     and to assert the table it is bound to, and 00011's keeps PUBLIC EXECUTE and asserts
--     nothing;
--   * api_keys_revoked_after_created closes BOUNDARY 1 here (a first stamp before the key existed);
--   * BOUNDARY 2 is the queries' (db/queries/apikeys.sql): every write to revoked_at carries
--     `revoked_at IS NULL` and COALESCE, so two revocations racing -- or a single one racing the
--     bulk one -- never reach the trigger, and the loser writes nothing rather than rolling the
--     winner's transaction back. TestApiKeyRevocation_ConcurrentAndBulkAreSafe measures it, and its
--     mutant (the guard removed) is red.
--
-- THE SEVENTH RESOLVER carries, never applies (internal/db/resolve.go): it returns the key's
-- tenant, its id, revoked_at and the tenant's api_enabled_at, and internal/apikey decides -- a
-- revoked key 401, a tenant whose API the operator has not opened 403 -- on every request, with no
-- cache. Its bound is the global UNIQUE on token_hash (at most one row) and the primary-key join
-- to tenants (one row of tenants per key).

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.punch_feed()') IS NULL
       OR pg_catalog.to_regclass('public.punch_events') IS NULL THEN
        RAISE EXCEPTION 'migration 00051 (M11 API-3) needs migrations 00048 to 00050 (the punch feed). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.api_keys') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.proname IN ('resolve_api_key_by_hash', 'api_keys_revocation_is_one_way')) THEN
        RAISE EXCEPTION 'migration 00051 (M11 API-3) creates api_keys and its two functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = 'tappa_resolver' AND rolbypassrls AND NOT rolsuper AND NOT rolcanlogin) THEN
        RAISE EXCEPTION 'migration 00051 (M11 API-3): role tappa_resolver must exist, NOLOGIN, NOSUPERUSER and BYPASSRLS (scripts/db-init/01-roles.sql). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- tappa_resolver's first reach into tenants is two COLUMNS (ADR 0002's M11 note). A grant on
    -- tenants it already held -- table level, or another column -- cannot be narrowed by a column
    -- grant, so it is refused here rather than silently kept.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                WHERE a.attrelid = 'public.tenants'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                  AND (pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')
                       OR pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'INSERT')
                       OR pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'UPDATE')))
       OR pg_catalog.has_table_privilege('tappa_resolver', 'public.tenants', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_resolver', 'public.tenants', 'TRUNCATE') THEN
        RAISE EXCEPTION 'migration 00051 (M11 API-3) gives tappa_resolver SELECT on two columns of tenants, and the role already reaches tenants through a grant no migration gave it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE api_keys (
    tenant_id    uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id           uuid        NOT NULL DEFAULT gen_random_uuid(),
    name         text        NOT NULL,
    token_hash   text        NOT NULL,
    prefix_hint  text        NOT NULL,
    created_by   uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz,
    CONSTRAINT api_keys_pkey PRIMARY KEY (id),
    -- GLOBAL, not per tenant (ADR 0028 §3): it is what bounds the resolver to one row.
    CONSTRAINT api_keys_token_hash_key UNIQUE (token_hash),
    CONSTRAINT api_keys_id_tenant_key UNIQUE (id, tenant_id),
    CONSTRAINT api_keys_creator_fk FOREIGN KEY (created_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT api_keys_token_hash_shape CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT api_keys_prefix_hint_shape CHECK (prefix_hint ~ '^tt_(live|test)_[A-Za-z0-9_-]{4}$'),
    -- A label the owner reads back in a list: one to 64 characters, no control character, no
    -- leading or trailing blank (the domain refuses the same before it gets here).
    CONSTRAINT api_keys_name_shape CHECK (
        char_length(name) BETWEEN 1 AND 64 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
    CONSTRAINT api_keys_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT api_keys_used_after_created CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);

-- The panel's list, a tenant's keys newest first; and the RLS five's index on tenant_id.
CREATE INDEX api_keys_tenant_created_idx ON api_keys (tenant_id, created_at DESC);

-- Two LIVE keys of one business cannot share a name (case aside). It is what makes a re-posted
-- creation form -- a reload, the back button -- a refusal rather than a second key nobody meant
-- to issue: the raw key is shown once and never again, so the form cannot be idempotent any
-- other way. A revoked key's name is free again.
CREATE UNIQUE INDEX api_keys_live_name_key ON api_keys (tenant_id, lower(name)) WHERE revoked_at IS NULL;

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY api_keys_tenant_isolation ON api_keys
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose StatementBegin
CREATE FUNCTION public.api_keys_revocation_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.api_keys'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'api_keys_revocation_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RAISE EXCEPTION 'api key revocation is one way: revoked_at may go from NULL to a timestamp, never back and never to another instant'
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.api_keys_revocation_is_one_way() FROM PUBLIC;

-- WHEN carries the whole condition, so an ordinary last_used_at write never enters the function,
-- and an idempotent COALESCE(revoked_at, ...) on a revoked row writes the same value (IS DISTINCT
-- FROM is false) and does not fire it either.
CREATE TRIGGER api_keys_revocation_one_way
    BEFORE UPDATE ON api_keys
    FOR EACH ROW
    WHEN (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
    EXECUTE FUNCTION public.api_keys_revocation_is_one_way();

-- tappa_app (ADR 0028 §1's matrix row): reads its keys -- every column but token_hash, which only
-- the resolver needs (narrower than the matrix's bare "SELECT"; the panel never reads a hash);
-- inserts a key by naming it, its hash, its hint and its issuer (id and created_at are the
-- defaults); updates revoked_at and last_used_at alone; never deletes.
REVOKE ALL ON api_keys FROM tappa_app;
GRANT SELECT (tenant_id, id, name, prefix_hint, created_by, created_at, last_used_at, revoked_at) ON api_keys TO tappa_app;
GRANT INSERT (tenant_id, name, token_hash, prefix_hint, created_by) ON api_keys TO tappa_app;
GRANT UPDATE (revoked_at, last_used_at) ON api_keys TO tappa_app;

-- tappa_resolver: the resolution's columns of api_keys, and ONLY (id, api_enabled_at) of tenants --
-- the primary-key join; name, vat_number and the rest stay unreadable (ADR 0002's M11 note).
GRANT SELECT (id, tenant_id, token_hash, revoked_at) ON api_keys TO tappa_resolver;
GRANT SELECT (id, api_enabled_at) ON tenants TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION public.resolve_api_key_by_hash(p_token_hash text)
    RETURNS TABLE (
        tenant_id      uuid,
        key_id         uuid,
        revoked_at     timestamptz,
        api_enabled_at timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT k.tenant_id, k.id, k.revoked_at, t.api_enabled_at
      FROM public.api_keys AS k
      JOIN public.tenants AS t ON t.id = k.tenant_id
     WHERE k.token_hash = p_token_hash;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_api_key_by_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_api_key_by_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_api_key_by_hash(text) TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.api_keys', 'token_hash', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.api_keys', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.api_keys', 'TRUNCATE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.api_keys'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('revoked_at', 'last_used_at')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR pg_catalog.has_function_privilege('tappa_app', 'public.api_keys_revocation_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_api_key_by_hash(text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_api_key_by_hash(text)', 'EXECUTE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.tenants'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('id', 'api_enabled_at')
                     AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')) THEN
        RAISE EXCEPTION 'migration 00051 (M11 API-3): tappa_app must not read token_hash, delete a key or update anything but revoked_at and last_used_at, must EXECUTE the resolver where PUBLIC does not, and tappa_resolver must read no column of tenants but id and api_enabled_at -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- The keys go with the table, which is the fail-closed direction: every integration's key stops
-- resolving. Their trail -- api_key.created / api_key.revoked -- is in audit_log and stays. It
-- names no operator object (opAtVersion skips it); its DROP TABLE takes tenants and admin_users
-- (the two keys' triggers), which holdingDownTables lists.
DROP FUNCTION IF EXISTS public.resolve_api_key_by_hash(text);
REVOKE SELECT (id, api_enabled_at) ON tenants FROM tappa_resolver;
DROP TABLE IF EXISTS api_keys;
DROP FUNCTION IF EXISTS public.api_keys_revocation_is_one_way();
