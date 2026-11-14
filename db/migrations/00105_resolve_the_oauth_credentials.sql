-- 00105 -- issue #37 phase 1: the three OAuth resolvers -- resolve_oauth_client,
-- resolve_oauth_code_by_hash, resolve_oauth_token_by_hash -- and tappa_resolver's reach for them.
--
-- NORMATIVE SOURCE: docs/adr/0047-oauth-bagli-uygulamalar.md §14.3 (the three functions, their
-- inputs and fixed result columns, ADR 0002 md.7's five constraints, resolve_oauth_client's named
-- deviation from md.7 (i), tappa_resolver's widening on tenants to api_code), §7.3 ("iptal
-- önbelleksiz": the token resolver carries the grant's revocation and the client's disabling on
-- every request), §11.4 (the operator's closing of a business's API is read from the schema:
-- api_enabled_at IS NULL AND api_code IS NOT NULL -- so the token resolver carries api_code);
-- docs/adr/0002-tenant-baglami-ve-rls.md md.7 and its M11 note; 00051 (the seventh resolver, this
-- file's shape).
--
-- CARRY, NEVER APPLY (00051): each function returns the row's facts and decides nothing -- an
-- expired, used or revoked token, a revoked grant, a disabled client, a closed API are all RETURNED;
-- internal/oauth and internal/apikey decide (401, 403, invalid_grant) in Go, on every request,
-- with no cache.
--
-- THE FIVE CONSTRAINTS (ADR 0002 md.7), each function:
--   (i)   input: the hash alone -- but resolve_oauth_client, whose input is the client id, a public
--         value that travels in URLs (ADR 0047 §14.3's named deviation: it resolves no tenant, the
--         table carries no tenant data, the columns it returns are the ones the consent page shows
--         anyway, and the secret is compared IN the function -- a boolean comes back, never the
--         hash; resolve_admin_owners_by_tenant is the precedent of a resolver keyed by an id);
--   (ii)  at most ONE row: code_hash and token_hash are globally UNIQUE (00104), oauth_clients is
--         keyed by its primary key, and every join is to a primary key;
--   (iii) a fixed column list -- no SELECT * surface, no hash returned;
--   (iv)  EXECUTE to tappa_app alone (PUBLIC revoked); tappa_app holds nothing on oauth_clients and
--         no hash column of the other tables (00104);
--   (v)   no "show rows when the context is NULL" branch: the tables' policies are the standard
--         NULLIF ones, and the functions run as tappa_resolver (BYPASSRLS) on named columns.
--
-- tappa_resolver's reach grows by these columns and nothing else: oauth_clients (id, name,
-- redirect_uris, allowed_scopes, secret_hash, disabled_at) -- the hash read for the comparison,
-- never returned; oauth_codes and oauth_tokens: the columns the two resolvers filter on and return;
-- oauth_grants (id, tenant_id, client_id, revoked_at) -- the token's grant, joined by its key; and
-- tenants (api_code), its third column there after 00051's (id, api_enabled_at). api_code is not a
-- secret (a punch publishes it).

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.oauth_tokens') IS NULL
       OR pg_catalog.to_regprocedure('public.resolve_api_key_by_hash(text)') IS NULL THEN
        RAISE EXCEPTION 'migration 00105 (issue #37) needs migration 00104 (the oauth tables) and 00051. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regprocedure('public.resolve_oauth_client(uuid, text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.resolve_oauth_code_by_hash(text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.resolve_oauth_token_by_hash(text)') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00105 (issue #37) creates the three oauth resolvers, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = 'tappa_resolver' AND rolbypassrls AND NOT rolsuper AND NOT rolcanlogin) THEN
        RAISE EXCEPTION 'migration 00105 (issue #37): role tappa_resolver must exist, NOLOGIN, NOSUPERUSER and BYPASSRLS (scripts/db-init/01-roles.sql). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- tappa_resolver reaches exactly 00051's two columns of tenants before this file. A wider grant
    -- another hand gave cannot be narrowed by a column grant, so it is refused rather than kept.
    IF (SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attname), '') FROM pg_catalog.pg_attribute a
         WHERE a.attrelid = 'public.tenants'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
           AND (pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')
                OR pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'INSERT')
                OR pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'UPDATE')))
         IS DISTINCT FROM 'api_enabled_at,id'
       OR pg_catalog.has_table_privilege('tappa_resolver', 'public.tenants', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_resolver', 'public.tenants', 'TRUNCATE')
       OR EXISTS (SELECT 1
                    FROM (VALUES ('public.oauth_clients'::pg_catalog.regclass), ('public.oauth_grants'::pg_catalog.regclass),
                                 ('public.oauth_codes'::pg_catalog.regclass), ('public.oauth_tokens'::pg_catalog.regclass)) AS w(rel)
                   WHERE pg_catalog.has_any_column_privilege('tappa_resolver', w.rel, 'SELECT')
                      OR pg_catalog.has_any_column_privilege('tappa_resolver', w.rel, 'INSERT')
                      OR pg_catalog.has_any_column_privilege('tappa_resolver', w.rel, 'UPDATE')
                      OR pg_catalog.has_table_privilege('tappa_resolver', w.rel, 'DELETE')
                      OR pg_catalog.has_table_privilege('tappa_resolver', w.rel, 'TRUNCATE')) THEN
        RAISE EXCEPTION 'migration 00105 (issue #37) gives tappa_resolver named columns of tenants and of the oauth tables, and the role already reaches them through a grant no migration gave it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

GRANT SELECT (id, name, redirect_uris, allowed_scopes, secret_hash, disabled_at) ON oauth_clients TO tappa_resolver;
GRANT SELECT (tenant_id, id, code_hash, client_id, consumed_at, expires_at, grant_id) ON oauth_codes TO tappa_resolver;
GRANT SELECT (tenant_id, id, grant_id, kind, token_hash, scopes, expires_at, used_at, revoked_at) ON oauth_tokens TO tappa_resolver;
GRANT SELECT (id, tenant_id, client_id, revoked_at) ON oauth_grants TO tappa_resolver;
GRANT SELECT (api_code) ON tenants TO tappa_resolver;

-- resolve_oauth_client: the app by its id -- what the consent page, the token endpoint's client
-- authentication, the invitation gate (§11.3 step 3) and the "Connected apps" names need. With a
-- secret hash, secret_ok says whether it is this app's; with NULL, secret_ok is NULL (the id-only
-- callers). The hash is compared here and never leaves the function. A disabled app is RETURNED
-- (disabled_at), not hidden: the caller says 401 invalid_client / "turned off by Taptime".
-- +goose StatementBegin
CREATE FUNCTION public.resolve_oauth_client(p_client_id uuid, p_secret_hash text)
    RETURNS TABLE (
        id             uuid,
        name           text,
        redirect_uris  text[],
        allowed_scopes text[],
        disabled_at    timestamptz,
        secret_ok      boolean
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT c.id, c.name, c.redirect_uris, c.allowed_scopes, c.disabled_at,
           CASE WHEN p_secret_hash IS NULL THEN NULL ELSE c.secret_hash = p_secret_hash END
      FROM public.oauth_clients AS c
     WHERE c.id = p_client_id;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_oauth_client(uuid, text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_oauth_client(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_oauth_client(uuid, text) TO tappa_app;

-- resolve_oauth_code_by_hash: whose code this is (§5.2 step 2). The consumption itself is the
-- one-statement UPDATE in WithTenant(the code's tenant) -- this only finds the tenant.
-- +goose StatementBegin
CREATE FUNCTION public.resolve_oauth_code_by_hash(p_code_hash text)
    RETURNS TABLE (
        tenant_id   uuid,
        code_id     uuid,
        client_id   uuid,
        consumed_at timestamptz,
        expires_at  timestamptz,
        grant_id    uuid
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT k.tenant_id, k.id, k.client_id, k.consumed_at, k.expires_at, k.grant_id
      FROM public.oauth_codes AS k
     WHERE k.code_hash = p_code_hash;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_oauth_code_by_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_oauth_code_by_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_oauth_code_by_hash(text) TO tappa_app;

-- resolve_oauth_token_by_hash: whose token this is, and every fact that decides whether a request
-- carrying it proceeds -- the token's own state, its grant's revocation, its app's disabling, the
-- business's K-22 columns (api_enabled_at AND api_code: "closed by the operator" is
-- `api_enabled_at IS NULL AND api_code IS NOT NULL`, §11.4) -- and the token's OWN scopes (an access
-- token narrowed at refresh carries fewer than its grant; /v1's scope check reads these, §8.1).
-- client_id is the grant's (the token has none of its own). Joins are by primary key, so the bound
-- stays token_hash's UNIQUE.
-- +goose StatementBegin
CREATE FUNCTION public.resolve_oauth_token_by_hash(p_token_hash text)
    RETURNS TABLE (
        tenant_id          uuid,
        token_id           uuid,
        kind               text,
        grant_id           uuid,
        client_id          uuid,
        token_scopes       text[],
        expires_at         timestamptz,
        used_at            timestamptz,
        token_revoked_at   timestamptz,
        grant_revoked_at   timestamptz,
        client_disabled_at timestamptz,
        api_enabled_at     timestamptz,
        api_code           text
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT k.tenant_id, k.id, k.kind, k.grant_id, g.client_id, k.scopes, k.expires_at, k.used_at,
           k.revoked_at, g.revoked_at, c.disabled_at, t.api_enabled_at, t.api_code
      FROM public.oauth_tokens AS k
      JOIN public.oauth_grants AS g ON g.id = k.grant_id AND g.tenant_id = k.tenant_id
      JOIN public.oauth_clients AS c ON c.id = g.client_id
      JOIN public.tenants AS t ON t.id = k.tenant_id
     WHERE k.token_hash = p_token_hash;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_oauth_token_by_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_oauth_token_by_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_oauth_token_by_hash(text) TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_function_privilege('public', 'public.resolve_oauth_client(uuid, text)', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_oauth_code_by_hash(text)', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_oauth_token_by_hash(text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_oauth_client(uuid, text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_oauth_code_by_hash(text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_oauth_token_by_hash(text)', 'EXECUTE')
       OR (SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attname), '') FROM pg_catalog.pg_attribute a
            WHERE a.attrelid = 'public.tenants'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
              AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT'))
           IS DISTINCT FROM 'api_code,api_enabled_at,id' THEN
        RAISE EXCEPTION 'migration 00105 (issue #37): the three oauth resolvers must be EXECUTE to tappa_app and not to PUBLIC, and tappa_resolver must read no column of tenants but id, api_enabled_at and api_code. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Every OAuth credential stops resolving (fail closed). The column grants go with the functions;
-- tappa_resolver is back at 00051's (id, api_enabled_at) on tenants.
DROP FUNCTION IF EXISTS public.resolve_oauth_token_by_hash(text);
DROP FUNCTION IF EXISTS public.resolve_oauth_code_by_hash(text);
DROP FUNCTION IF EXISTS public.resolve_oauth_client(uuid, text);
REVOKE SELECT (api_code) ON tenants FROM tappa_resolver;
REVOKE SELECT (tenant_id, id, client_id, revoked_at) ON oauth_grants FROM tappa_resolver;
REVOKE SELECT (tenant_id, id, grant_id, kind, token_hash, scopes, expires_at, used_at, revoked_at) ON oauth_tokens FROM tappa_resolver;
REVOKE SELECT (tenant_id, id, code_hash, client_id, consumed_at, expires_at, grant_id) ON oauth_codes FROM tappa_resolver;
REVOKE SELECT (id, name, redirect_uris, allowed_scopes, secret_hash, disabled_at) ON oauth_clients FROM tappa_resolver;
