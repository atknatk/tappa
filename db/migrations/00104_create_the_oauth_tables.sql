-- 00104 -- issue #37 phase 1: the OAuth 2.0 "Connected apps" tables -- the app registry
-- (oauth_clients, tenant-scope-less) and the three tenant tables (oauth_grants, oauth_codes,
-- oauth_tokens) with their one-way triggers.
--
-- NORMATIVE SOURCE: docs/adr/0047-oauth-bagli-uygulamalar.md §9 (one grant, one tenant; the
-- registry carries no tenant data), §10.1-§10.2 (the registry and its redirect URI rule, in Go AND
-- in SQL), §12 (revocation is one way -- 00051's api_keys_revocation_is_one_way precedent), §14.1
-- (oauth_clients), §14.2 (the three tenant tables: one clock, equality CHECKs, hours not days,
-- column-listed grants), §5.2-§5.3 (what the consumption and the rotation statements need of them);
-- CLAUDE.md §4.5 and §6 (the five, the GRANT, the tenant-scope-less table's rules).
--
-- WHY FOUR FILES IN THIS ORDER (a deviation from ADR 0047 §14.6's list, which puts the registry and
-- the op_* in the first file). internal/db's opAtVersion walks back every migration that names an
-- operator object, inside an operator test's transaction, and skips the rest
-- (TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject). The registry's operator half --
-- tappa_opdefiner's grants, the key to platform_admins, the audit kinds and the five op_* -- is
-- 00107's; this file, 00105 (the resolvers) and 00106 (api_idempotency) name no operator object, so
-- an operator test reaching an older version walks 00107's Down alone and never drops a table other
-- packages' tests write. The tables exist before the functions that write them, which is the only
-- order ADR 0047 actually depends on.
--
-- 🔴 oauth_clients IS TENANT-SCOPE-LESS AND CARRIES NO TENANT DATA (ADR 0047 §9, §10.1): an app the
-- operator registered once for every business. No column is named tenant_id (R5 and the RLS tests
-- read that name as "tenant table"). ENABLE and FORCE ROW LEVEL SECURITY with NO policy: nobody
-- subject to RLS reads a row -- tappa_resolver (00105) and tappa_opdefiner (00107) are BYPASSRLS and
-- each reaches named columns only. tappa_app holds NOTHING on it: the default ACL of dev and
-- production (scripts/db-init/03) would hand it arwd, so the REVOKE below is explicit.
--
-- 🔴 THE THREE TENANT TABLES (CLAUDE.md §6's five + GRANT): tenant_id NOT NULL, an index led by
-- tenant_id, ENABLE + FORCE, the NULLIF isolation policy with USING and WITH CHECK, and tappa_app's
-- grants by COLUMN. A code and a token reach their grant through the composite key (grant_id,
-- tenant_id) -> oauth_grants (id, tenant_id): a row can never name another tenant's grant (§9).
--
-- ONE CLOCK PER ROW (ADR 0047 §14.2): created_at and expires_at come from the same read, now() --
-- the transaction's start, one value for the whole transaction -- and the CHECK is an EQUALITY, not
-- a ceiling: a code lives exactly 60 seconds, an access token exactly 1 hour, a refresh token
-- exactly 2160 hours. Intervals are in seconds and hours, never days: `timestamptz + '90 days'`
-- depends on the session's TimeZone (a DST change), and a CHECK is re-evaluated on every UPDATE of
-- the row -- a revoked_at written from a session in another zone would then be refused. A Go clock
-- enters no lifetime: created_at is never in tappa_app's INSERT list, and expires_at is in the
-- tokens' list only because a DEFAULT cannot read another column (kind); the equality CHECK binds it
-- to the one value.
--
-- ONE-WAY STATE (00051's rule, BOUNDARY 1 and 2): every "it happened" stamp -- a client's
-- disabled_at, a grant's revocation, a code's consumption and the grant it bore, a token's use and
-- revocation -- goes from NULL to a value once and never back or elsewhere. Each table's trigger is
-- its own function (EXECUTE in its owner alone, asserting TG_RELID and its event). The queries
-- (db/queries/oauth.sql) carry `... IS NULL` and COALESCE on every such write, so two racing writes
-- never reach the trigger and the loser writes nothing.
--
-- No value here is a secret: every secret, code and token is stored as the hex HMAC-SHA256 under
-- TAPPA_OAUTH_HMAC_KEY (ADR 0047 §7.2), never the value; the hashes are globally UNIQUE, which is
-- what bounds each resolver of 00105 to one row (ADR 0002 md.7 (ii)).

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.api_idempotency') IS NULL
       OR pg_catalog.to_regclass('public.admin_users') IS NULL THEN
        RAISE EXCEPTION 'migration 00104 (issue #37) needs migrations up to 00103. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.oauth_clients') IS NOT NULL
       OR pg_catalog.to_regclass('public.oauth_grants') IS NOT NULL
       OR pg_catalog.to_regclass('public.oauth_codes') IS NOT NULL
       OR pg_catalog.to_regclass('public.oauth_tokens') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                     AND p.proname IN ('oauth_clients_disabling_is_one_way', 'oauth_grants_revocation_is_one_way',
                                       'oauth_codes_spending_is_one_way', 'oauth_tokens_state_is_one_way')) THEN
        RAISE EXCEPTION 'migration 00104 (issue #37) creates the four oauth_* tables and their four trigger functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The redirect URI rule, in SQL (ADR 0047 §10.2: Go AND SQL, defence in depth).
-- ---------------------------------------------------------------------------
-- ONE fixed regular expression -- written here in oauth_clients_redirect_uris_shape and, byte for
-- byte, in 00107's op_register_oauth_client and op_set_oauth_client_redirects (internal/db's
-- TestOAuth_TheRedirectRuleIsOneExpression holds the three copies equal). A list is valid when it is a
-- one-dimensional array, indexed from 1, of 1 to 5 addresses, none NULL, none longer than 512 bytes,
-- each either
--   https://<lower-case DNS name with at least one dot, its last label starting with a letter --
--            so no IP literal and no `[`>[:443]/<path>
--   http://(localhost|127.0.0.1):<port 1-99999>/<path>     (the loopback form, §10.4)
-- and the path drawn from RFC 3986's unreserved and sub-delim characters plus `:`, `%` and `/` --
-- so no `?` (no query), no `#` (no fragment), no `@` (no userinfo), no space, no control.
-- The loopback form is accepted HERE in every build: the database has no mark of the sandbox build
-- (ADR 0047 §10.2, measured), so the build rule is Go's -- the operator surface refuses loopback
-- outside `-tags sandbox`, and the production authorization endpoint refuses every non-https
-- redirect (V2) whatever a row holds.
-- WHY FIVE INDEXED TERMS, NOT ONE OVER THE ARRAY: a CHECK cannot iterate an array without a
-- subquery, a regular expression cannot bound an element to 512 bytes (PostgreSQL's repetition bound
-- is 255), and a helper function would need EXECUTE for every writing role -- tappa_opdefiner among
-- them, whose foreign EXECUTE set is pinned (ADR 0021 §1). The list holds at most five, so the CHECK
-- names each slot; the whole is wrapped in coalesce so a NULL element is a refusal, not a pass.

-- ---------------------------------------------------------------------------
-- 2. oauth_clients -- the registry (ADR 0047 §14.1). Tenant-scope-less.
-- ---------------------------------------------------------------------------
-- redline: no-tenant-scope(oauth_clients) — the platform operator's app registry; one row per app for every business, no tenant data; written by op_* alone (00107), read by tappa_resolver's resolve_oauth_client and op_read_oauth_clients alone (ADR 0047 §9, §10.1, §14.1)
CREATE TABLE oauth_clients (
    id                uuid        NOT NULL DEFAULT gen_random_uuid(),
    name              text        NOT NULL,
    redirect_uris     text[]      NOT NULL,
    allowed_scopes    text[]      NOT NULL,
    secret_hash       text        NOT NULL,
    secret_hint       text        NOT NULL,
    secret_rotated_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- The operator who registered it (platform_admins.id). Its key is 00107's: the registry's
    -- operator half lives in one file (the header).
    created_by        uuid        NOT NULL,
    disabled_at       timestamptz,
    CONSTRAINT oauth_clients_pkey PRIMARY KEY (id),
    -- GLOBAL: one secret, one app (the resolver compares by equality).
    CONSTRAINT oauth_clients_secret_hash_key UNIQUE (secret_hash),
    CONSTRAINT oauth_clients_secret_hash_shape CHECK (secret_hash ~ '^[0-9a-f]{64}$'),
    -- The secret's prefix and the four characters after it (api_keys.prefix_hint's rule): enough
    -- to tell a rotation apart, too few to shrink a guess.
    CONSTRAINT oauth_clients_secret_hint_shape CHECK (secret_hint ~ '^tt_cs_(live|test)_[A-Za-z0-9_-]{4}$'),
    -- The name the owner reads on the consent page: 1 to 64 characters, trimmed, and the business
    -- name's isolate rule (00093's set -- controls, the line and paragraph separators, the bidi
    -- embeddings, overrides and isolates).
    CONSTRAINT oauth_clients_name_shape CHECK (
        char_length(name) BETWEEN 1 AND 64 AND name = btrim(name)
        AND name !~ '[\u0001-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069]'),
    CONSTRAINT oauth_clients_redirect_uris_shape CHECK (coalesce(
        array_ndims(redirect_uris) = 1 AND array_lower(redirect_uris, 1) = 1
        AND cardinality(redirect_uris) BETWEEN 1 AND 5
        AND octet_length(redirect_uris[1]) <= 512 AND redirect_uris[1] ~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$'
        AND (cardinality(redirect_uris) < 2
             OR (octet_length(redirect_uris[2]) <= 512 AND redirect_uris[2] ~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$'))
        AND (cardinality(redirect_uris) < 3
             OR (octet_length(redirect_uris[3]) <= 512 AND redirect_uris[3] ~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$'))
        AND (cardinality(redirect_uris) < 4
             OR (octet_length(redirect_uris[4]) <= 512 AND redirect_uris[4] ~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$'))
        AND (cardinality(redirect_uris) < 5
             OR (octet_length(redirect_uris[5]) <= 512 AND redirect_uris[5] ~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$')),
        false)),
    -- The closed scope set of ADR 0047 §8.2. webhooks:manage is NOT in it (§13): this delivery
    -- cannot register it. Not empty, one dimension, no NULL element.
    CONSTRAINT oauth_clients_allowed_scopes_shape CHECK (
        array_ndims(allowed_scopes) = 1 AND cardinality(allowed_scopes) > 0
        AND array_position(allowed_scopes, NULL) IS NULL
        AND allowed_scopes <@ '{timesheets:read,employees:read,employees:write,locations:write,punches:read}'::text[]),
    CONSTRAINT oauth_clients_rotated_after_created CHECK (secret_rotated_at IS NULL OR secret_rotated_at >= created_at),
    CONSTRAINT oauth_clients_disabled_after_created CHECK (disabled_at IS NULL OR disabled_at >= created_at)
);

ALTER TABLE oauth_clients ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_clients FORCE ROW LEVEL SECURITY;

-- +goose StatementBegin
CREATE FUNCTION public.oauth_clients_disabling_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.oauth_clients'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'oauth_clients_disabling_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RAISE EXCEPTION 'oauth client disabling is one way: disabled_at may go from NULL to a timestamp, never back and never to another instant'
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.oauth_clients_disabling_is_one_way() FROM PUBLIC;

CREATE TRIGGER oauth_clients_disabling_one_way
    BEFORE UPDATE ON oauth_clients
    FOR EACH ROW
    WHEN (OLD.disabled_at IS NOT NULL AND NEW.disabled_at IS DISTINCT FROM OLD.disabled_at)
    EXECUTE FUNCTION public.oauth_clients_disabling_is_one_way();

REVOKE ALL ON oauth_clients FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 3. oauth_grants -- one business's consent to one app (ADR 0047 §14.2, §5.2, §12).
-- ---------------------------------------------------------------------------
CREATE TABLE oauth_grants (
    tenant_id      uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id             uuid        NOT NULL DEFAULT gen_random_uuid(),
    client_id      uuid        NOT NULL,
    scopes         text[]      NOT NULL,
    -- The owner's membership that approved (the business's grant, not the person's -- §12).
    created_by     uuid        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_used_at   timestamptz,
    revoked_at     timestamptz,
    revoked_reason text,
    -- The membership that disconnected it, when one did (NULL for the app's revoke, a supersede, a
    -- reuse detection).
    revoked_by     uuid,
    CONSTRAINT oauth_grants_pkey PRIMARY KEY (id),
    CONSTRAINT oauth_grants_id_tenant_key UNIQUE (id, tenant_id),
    CONSTRAINT oauth_grants_client_fk FOREIGN KEY (client_id)
        REFERENCES oauth_clients (id) ON DELETE RESTRICT,
    CONSTRAINT oauth_grants_creator_fk FOREIGN KEY (created_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT oauth_grants_revoker_fk FOREIGN KEY (revoked_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT oauth_grants_scopes_shape CHECK (
        array_ndims(scopes) = 1 AND cardinality(scopes) > 0
        AND array_position(scopes, NULL) IS NULL
        AND scopes <@ '{timesheets:read,employees:read,employees:write,locations:write,punches:read}'::text[]),
    CONSTRAINT oauth_grants_revoked_reason_check CHECK (
        revoked_reason IN ('owner', 'client', 'superseded', 'refresh_reuse', 'code_reuse')),
    CONSTRAINT oauth_grants_revocation_both_or_neither CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
    CONSTRAINT oauth_grants_revoker_needs_a_revocation CHECK (revoked_by IS NULL OR revoked_at IS NOT NULL),
    CONSTRAINT oauth_grants_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT oauth_grants_used_after_created CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);

-- The "Connected apps" list, a tenant's grants newest first; and the RLS five's index on tenant_id.
CREATE INDEX oauth_grants_tenant_created_idx ON oauth_grants (tenant_id, created_at DESC);

-- O-8: one LIVE grant per (business, app). A second approval supersedes the first in the exchange's
-- transaction (§5.2 step 7); two exchanges racing for one pair meet here and the loser rolls back.
CREATE UNIQUE INDEX oauth_grants_live_client_key ON oauth_grants (tenant_id, client_id) WHERE revoked_at IS NULL;

ALTER TABLE oauth_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY oauth_grants_tenant_isolation ON oauth_grants
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose StatementBegin
CREATE FUNCTION public.oauth_grants_revocation_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.oauth_grants'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'oauth_grants_revocation_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RAISE EXCEPTION 'oauth grant revocation is one way: revoked_at, revoked_reason and revoked_by are written once, never changed or cleared'
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.oauth_grants_revocation_is_one_way() FROM PUBLIC;

-- WHEN carries the whole condition: last_used_at writes never enter the function, and an idempotent
-- COALESCE on a revoked row writes the same values (IS DISTINCT FROM false).
CREATE TRIGGER oauth_grants_revocation_one_way
    BEFORE UPDATE ON oauth_grants
    FOR EACH ROW
    WHEN (OLD.revoked_at IS NOT NULL
          AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
               OR NEW.revoked_reason IS DISTINCT FROM OLD.revoked_reason
               OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by))
    EXECUTE FUNCTION public.oauth_grants_revocation_is_one_way();

-- ---------------------------------------------------------------------------
-- 4. oauth_codes -- the authorization code, 60 seconds, one use (ADR 0047 §14.2, §5.2).
-- ---------------------------------------------------------------------------
CREATE TABLE oauth_codes (
    tenant_id      uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id             uuid        NOT NULL DEFAULT gen_random_uuid(),
    code_hash      text        NOT NULL,
    client_id      uuid        NOT NULL,
    -- The owner's membership that approved; the exchange re-checks it (§5.2 step 6).
    created_by     uuid        NOT NULL,
    -- The registered address the code was issued for, byte for byte (V2).
    redirect_uri   text        NOT NULL,
    scopes         text[]      NOT NULL,
    code_challenge text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL DEFAULT now() + interval '60 seconds',
    consumed_at    timestamptz,
    -- The grant the code bore, written in the exchange's transaction; what a later presentation of
    -- the spent code revokes (§5.2 step 4).
    grant_id       uuid,
    CONSTRAINT oauth_codes_pkey PRIMARY KEY (id),
    CONSTRAINT oauth_codes_id_tenant_key UNIQUE (id, tenant_id),
    -- GLOBAL (the resolver's bound, ADR 0002 md.7 (ii)).
    CONSTRAINT oauth_codes_code_hash_key UNIQUE (code_hash),
    CONSTRAINT oauth_codes_client_fk FOREIGN KEY (client_id)
        REFERENCES oauth_clients (id) ON DELETE RESTRICT,
    CONSTRAINT oauth_codes_creator_fk FOREIGN KEY (created_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT oauth_codes_grant_fk FOREIGN KEY (grant_id, tenant_id)
        REFERENCES oauth_grants (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT oauth_codes_code_hash_shape CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    -- BASE64URL(SHA-256(verifier)) without padding: 43 characters (RFC 7636 §4.2, S256 only).
    CONSTRAINT oauth_codes_code_challenge_shape CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
    CONSTRAINT oauth_codes_redirect_uri_shape CHECK (octet_length(redirect_uri) BETWEEN 1 AND 512),
    CONSTRAINT oauth_codes_scopes_shape CHECK (
        array_ndims(scopes) = 1 AND cardinality(scopes) > 0
        AND array_position(scopes, NULL) IS NULL
        AND scopes <@ '{timesheets:read,employees:read,employees:write,locations:write,punches:read}'::text[]),
    -- The one lifetime, by equality (the header's ONE CLOCK).
    CONSTRAINT oauth_codes_lifetime CHECK (expires_at = created_at + interval '60 seconds'),
    CONSTRAINT oauth_codes_consumed_after_created CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CONSTRAINT oauth_codes_grant_needs_a_consumption CHECK (grant_id IS NULL OR consumed_at IS NOT NULL)
);

-- The RLS five's index; also the lazy delete's walk (a tenant's oldest codes first).
CREATE INDEX oauth_codes_tenant_created_idx ON oauth_codes (tenant_id, created_at);

ALTER TABLE oauth_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY oauth_codes_tenant_isolation ON oauth_codes
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
-- The delete's reach, in the database (T130's webhook_deliveries_delete_settled precedent): the
-- context's tenant and a code that expired more than a day ago -- a spent code stays a day so a
-- late re-presentation is still a reuse (§5.2 step 4) rather than an unknown code.
CREATE POLICY oauth_codes_delete_expired ON oauth_codes
    AS RESTRICTIVE
    FOR DELETE
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
           AND expires_at < statement_timestamp() - interval '24 hours');

-- +goose StatementBegin
CREATE FUNCTION public.oauth_codes_spending_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.oauth_codes'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'oauth_codes_spending_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RAISE EXCEPTION 'oauth code spending is one way: consumed_at and grant_id are written once, never changed or cleared'
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.oauth_codes_spending_is_one_way() FROM PUBLIC;

-- A spent code un-spent would be a second exchange: the stamp and the grant it bore are one way.
CREATE TRIGGER oauth_codes_spending_one_way
    BEFORE UPDATE ON oauth_codes
    FOR EACH ROW
    WHEN ((OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at)
          OR (OLD.grant_id IS NOT NULL AND NEW.grant_id IS DISTINCT FROM OLD.grant_id))
    EXECUTE FUNCTION public.oauth_codes_spending_is_one_way();

-- ---------------------------------------------------------------------------
-- 5. oauth_tokens -- access (1 hour) and refresh (2160 hours) tokens (ADR 0047 §14.2, §5.3).
-- ---------------------------------------------------------------------------
-- No lineage column (parent_id): the tolerance rule revokes every live token of the grant and never
-- reads lineage (§5.3).
CREATE TABLE oauth_tokens (
    tenant_id  uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id         uuid        NOT NULL DEFAULT gen_random_uuid(),
    grant_id   uuid        NOT NULL,
    kind       text        NOT NULL,
    token_hash text        NOT NULL,
    -- The ACCESS token's own scope (a refresh may narrow it -- §5.3); NULL on a refresh token, which
    -- always carries the grant's.
    scopes     text[],
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    revoked_at timestamptz,
    CONSTRAINT oauth_tokens_pkey PRIMARY KEY (id),
    CONSTRAINT oauth_tokens_id_tenant_key UNIQUE (id, tenant_id),
    -- GLOBAL (the resolver's bound).
    CONSTRAINT oauth_tokens_token_hash_key UNIQUE (token_hash),
    CONSTRAINT oauth_tokens_grant_fk FOREIGN KEY (grant_id, tenant_id)
        REFERENCES oauth_grants (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT oauth_tokens_kind_check CHECK (kind IN ('access', 'refresh')),
    CONSTRAINT oauth_tokens_token_hash_shape CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT oauth_tokens_scopes_on_access_only CHECK ((kind = 'access') = (scopes IS NOT NULL)),
    CONSTRAINT oauth_tokens_scopes_shape CHECK (
        scopes IS NULL
        OR (array_ndims(scopes) = 1 AND cardinality(scopes) > 0
            AND array_position(scopes, NULL) IS NULL
            AND scopes <@ '{timesheets:read,employees:read,employees:write,locations:write,punches:read}'::text[])),
    -- The one lifetime per kind, by equality. (An unknown kind makes the CASE NULL, and
    -- oauth_tokens_kind_check refuses it.)
    CONSTRAINT oauth_tokens_lifetime CHECK (
        expires_at = created_at + CASE kind WHEN 'access' THEN interval '1 hour'
                                            WHEN 'refresh' THEN interval '2160 hours' END),
    CONSTRAINT oauth_tokens_use_on_refresh_only CHECK (kind = 'refresh' OR used_at IS NULL),
    CONSTRAINT oauth_tokens_used_after_created CHECK (used_at IS NULL OR used_at >= created_at),
    CONSTRAINT oauth_tokens_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

-- The RLS five's index, led by tenant_id, and the grant's walk: the tolerance's revocation of the
-- grant's live tokens, the invitation gate's "a live refresh token", the lazy cleanup.
CREATE INDEX oauth_tokens_tenant_grant_idx ON oauth_tokens (tenant_id, grant_id, kind);

ALTER TABLE oauth_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY oauth_tokens_tenant_isolation ON oauth_tokens
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
-- The delete's reach (ADR 0047 §14.2): an access token that expired more than an hour ago; a
-- refresh token past its 2160 hours (used, revoked or never presented -- each is dead by then).
CREATE POLICY oauth_tokens_delete_expired ON oauth_tokens
    AS RESTRICTIVE
    FOR DELETE
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
           AND ((kind = 'access' AND expires_at < statement_timestamp() - interval '1 hour')
                OR (kind = 'refresh' AND expires_at < statement_timestamp())));

-- +goose StatementBegin
CREATE FUNCTION public.oauth_tokens_state_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.oauth_tokens'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'oauth_tokens_state_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RAISE EXCEPTION 'oauth token state is one way: used_at and revoked_at go from NULL to a timestamp once, never back and never to another instant'
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.oauth_tokens_state_is_one_way() FROM PUBLIC;

-- used_at is one way too: the 30-second tolerance counts from a refresh token's FIRST use and must
-- not be stretched by a later write (§5.3 step 4).
CREATE TRIGGER oauth_tokens_state_one_way
    BEFORE UPDATE ON oauth_tokens
    FOR EACH ROW
    WHEN ((OLD.used_at IS NOT NULL AND NEW.used_at IS DISTINCT FROM OLD.used_at)
          OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at))
    EXECUTE FUNCTION public.oauth_tokens_state_is_one_way();

-- ---------------------------------------------------------------------------
-- 6. tappa_app (ADR 0047 §14.2's rows; the `arwd` rule of ADR 0028 §1): REVOKE ALL, then the verbs
--    and the COLUMNS. Every timestamp a lifetime or a ceiling reads is a DEFAULT and outside the
--    INSERT lists (but oauth_tokens.expires_at, bound by its CHECK). No hash column is readable:
--    the resolvers (00105) are the one reader of code_hash and token_hash.
-- ---------------------------------------------------------------------------
REVOKE ALL ON oauth_grants FROM tappa_app;
GRANT SELECT (tenant_id, id, client_id, scopes, created_by, created_at, last_used_at, revoked_at, revoked_reason, revoked_by)
    ON oauth_grants TO tappa_app;
GRANT INSERT (tenant_id, client_id, scopes, created_by) ON oauth_grants TO tappa_app;
GRANT UPDATE (last_used_at, revoked_at, revoked_reason, revoked_by) ON oauth_grants TO tappa_app;

REVOKE ALL ON oauth_codes FROM tappa_app;
GRANT SELECT (tenant_id, id, client_id, created_by, redirect_uri, scopes, code_challenge, created_at, expires_at, consumed_at, grant_id)
    ON oauth_codes TO tappa_app;
GRANT INSERT (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge) ON oauth_codes TO tappa_app;
GRANT UPDATE (consumed_at, grant_id) ON oauth_codes TO tappa_app;
GRANT DELETE ON oauth_codes TO tappa_app;

REVOKE ALL ON oauth_tokens FROM tappa_app;
GRANT SELECT (tenant_id, id, grant_id, kind, scopes, created_at, expires_at, used_at, revoked_at)
    ON oauth_tokens TO tappa_app;
GRANT INSERT (tenant_id, grant_id, kind, token_hash, scopes, expires_at) ON oauth_tokens TO tappa_app;
GRANT UPDATE (used_at, revoked_at) ON oauth_tokens TO tappa_app;
GRANT DELETE ON oauth_tokens TO tappa_app;

-- THE REACH, MEASURED (00051's closing block): through PUBLIC or any membership, tappa_app reaches
-- nothing of oauth_clients, no hash column, no column beyond the lists above, and no trigger function.
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_any_column_privilege('tappa_app', 'public.oauth_clients', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.oauth_clients', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.oauth_clients', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.oauth_clients', 'REFERENCES')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.oauth_clients', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.oauth_clients', 'TRUNCATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.oauth_clients', 'TRIGGER')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.oauth_codes', 'code_hash', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.oauth_tokens', 'token_hash', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.oauth_grants', 'DELETE')
       OR EXISTS (SELECT 1
                    FROM (VALUES ('public.oauth_grants'::pg_catalog.regclass,
                                  ARRAY['last_used_at', 'revoked_at', 'revoked_reason', 'revoked_by']),
                                 ('public.oauth_codes'::pg_catalog.regclass, ARRAY['consumed_at', 'grant_id']),
                                 ('public.oauth_tokens'::pg_catalog.regclass, ARRAY['used_at', 'revoked_at'])) AS w(rel, cols)
                    JOIN pg_catalog.pg_attribute a ON a.attrelid = w.rel AND a.attnum > 0 AND NOT a.attisdropped
                   WHERE a.attname <> ALL (w.cols)
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR EXISTS (SELECT 1
                    FROM (VALUES ('public.oauth_grants'::pg_catalog.regclass),
                                 ('public.oauth_codes'::pg_catalog.regclass),
                                 ('public.oauth_tokens'::pg_catalog.regclass)) AS w(rel)
                   WHERE pg_catalog.has_table_privilege('tappa_app', w.rel, 'TRUNCATE')
                      OR pg_catalog.has_table_privilege('tappa_app', w.rel, 'REFERENCES')
                      OR pg_catalog.has_table_privilege('tappa_app', w.rel, 'TRIGGER'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid IN ('public.oauth_grants'::pg_catalog.regclass, 'public.oauth_codes'::pg_catalog.regclass,
                                        'public.oauth_tokens'::pg_catalog.regclass)
                     AND a.attname IN ('created_at')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'INSERT'))
       OR pg_catalog.has_function_privilege('tappa_app', 'public.oauth_clients_disabling_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.oauth_grants_revocation_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.oauth_codes_spending_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.oauth_tokens_state_is_one_way()', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00104 (issue #37): tappa_app must reach nothing of oauth_clients, no hash column, no column beyond its lists and no oauth_* function -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Every grant, code and token goes with its table -- the fail-closed direction: every connected app
-- stops resolving at once (00051's Down). The registry goes last (the tenant tables' keys name it).
-- The audit trail -- oauth.* rows in audit_log -- stays. Its DROP TABLEs take tenants and admin_users
-- (the tables' keys) besides the four tables themselves.
DROP TABLE IF EXISTS oauth_tokens;
DROP TABLE IF EXISTS oauth_codes;
DROP TABLE IF EXISTS oauth_grants;
DROP TABLE IF EXISTS oauth_clients;
DROP FUNCTION IF EXISTS public.oauth_tokens_state_is_one_way();
DROP FUNCTION IF EXISTS public.oauth_codes_spending_is_one_way();
DROP FUNCTION IF EXISTS public.oauth_grants_revocation_is_one_way();
DROP FUNCTION IF EXISTS public.oauth_clients_disabling_is_one_way();
